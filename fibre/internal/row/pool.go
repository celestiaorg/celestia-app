// Package row provides a bucketed allocator of fixed-shape row slabs.
//
// A slab is the indivisible allocation unit: rowCount rows of rowSize
// bytes each. A Get(rowCount, rowSize) is served from the exact bucket
// or from a larger-rowSize bucket within a slack window on the same
// rowCount — never smaller. Failing both, a new slab is allocated at
// the exact requested key.
//
// Retention is demand-driven: each bucket has a generation counter that
// advances on every Get to that bucket. A free slab becomes evictable
// once bucket.gen exceeds its lastPut by evictionThreshold. When a
// bucket goes fully idle (no in-use slabs), an independent per-bucket
// idle timer arms and drops that bucket's free slabs after idleGrace
// if no Get arrives.
//
// Contract: buffers from [Pool.Get] must be returned together via
// [Pool.Put] to the same Pool that issued them; callers must not
// re-slice or split across calls. Put validates a back-pointer embedded
// in each slab's region and panics on double-free or already-released
// slabs. Passing buffers from a different Pool instance is undefined
// behavior. A pointer whose backing array lies near the start of a page
// can fault when Put reads the header below it, so callers must not
// pass buffers this pool never allocated.
package row

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/klauspost/reedsolomon"
)

// Structural constants — invariants of the memory layout, not tuning knobs.
const (
	// rowSizeAlign is the quantum for both rowSize and the minimum
	// accepted rowSize; SIMD-aligned to 64 bytes.
	rowSizeAlign = 64

	// headerSize prefixes every slab's backing region. Sized to keep
	// data 64-byte (SIMD) aligned; the first word is a back-pointer to
	// the owning *slab, used by Put to recover it from a carved buffer.
	headerSize = 64
)

// Pool tunables — safe to adjust, governed by workload characteristics.
const (
	// slack: max number of rowSizeAlign slot steps above the exact match
	// a free slab may occupy for upward fallback. Waste per reused slab
	// is slack*rowSizeAlign*rowCount bytes — constant per row, so safe to
	// apply at any rowSize. With defaults and Celestia's shape: ≤1.5 MiB
	// on the assembler slab, ≤4 MiB on the codec-work slab (~5.5MiB total)
	slack = 2

	// evictionThreshold: generation ticks a free slab must age through
	// before becoming evictable.
	evictionThreshold = 4

	// idleGrace: wait after a bucket goes fully idle before its free
	// slabs are dropped.
	idleGrace = 30 * time.Second

	// mmapThreshold: minimum slab size routed through mmap instead of
	// the Go heap. Above this, allocations stay invisible to GOGC's
	// heap-doubling pacer so the codec's multi-MiB work buffers don't
	// trigger OOMs.
	mmapThreshold = 1 << 20 // 1 MiB
)

// Pool is a bucketed allocator of fixed-shape row slabs for a single
// row count. Callers needing multiple row counts construct one Pool
// each.
//
// Buckets live by-value in a flat slice indexed by rowSize/64 - 1, so
// exact lookup is a single index. Concurrency is bucket-local: Pool's
// own state is fixed at [NewPool] and never mutates; each bucket carries
// its own mutex and idle-drop timer. Disjoint buckets serve Get/Put in
// parallel.
//
// O(1) for both Put and Get. Safe for concurrent use.
// Implements [reedsolomon.WorkAllocator].
type Pool struct {
	rowCount   int
	maxRowSize int

	// buckets[sizeIdx] is the bucket for rowSize (sizeIdx+1)*rowSizeAlign.
	buckets []bucket
}

// bucket holds free and in-use slabs for one (rowCount, rowSize)
// pair. Every state mutation is serialized by the embedded mutex.
//
// free is a FIFO by release time: reuse pops the back (LIFO warmth),
// aged eviction pops the front (oldest first). used anchors live
// in-use slabs on the Go heap — the header back-pointer inside a
// (possibly mmap'd) region is invisible to GC, so a Go-heap reference
// must outlive the slab. idleTimer arms when used goes empty and
// drops free slabs after idleGrace if no Get arrives.
type bucket struct {
	sync.Mutex

	gen        uint64
	free, used []*slab
	idleTimer  *time.Timer
}

// NewPool creates a Pool serving the given rowCount. maxRowSize must
// be a positive multiple of 64; rowCount must be positive.
func NewPool(maxRowSize, rowCount int) *Pool {
	if maxRowSize < rowSizeAlign || maxRowSize%rowSizeAlign != 0 {
		panic(fmt.Sprintf("row: maxRowSize %d must be a positive multiple of %d", maxRowSize, rowSizeAlign))
	}
	if rowCount <= 0 {
		panic(fmt.Sprintf("row: rowCount %d must be positive", rowCount))
	}
	return &Pool{
		rowCount:   rowCount,
		maxRowSize: maxRowSize,
		buckets:    make([]bucket, maxRowSize/rowSizeAlign),
	}
}

// Get returns n buffers of size bytes each, carved from one slab.
// n must equal the pool's rowCount; size must be a positive multiple
// of 64 in [64, maxRowSize]. Violations panic.
func (p *Pool) Get(n, size int) [][]byte {
	if n == 0 {
		return nil
	}
	if n != p.rowCount {
		panic(fmt.Sprintf("row: rowCount %d does not match pool's %d", n, p.rowCount))
	}
	if size < rowSizeAlign || size > p.maxRowSize || size%rowSizeAlign != 0 {
		panic(fmt.Sprintf("row: size %d invalid (want positive multiple of %d, <= %d)", size, rowSizeAlign, p.maxRowSize))
	}

	bk, s := p.pop(size)
	if s == nil {
		s = bk.new(n * size)
	}
	return s.carve(n, size)
}

// Put returns the slab backing bufs to its owning bucket. Panics on
// empty bufs, an already-released slab, or a double-free. Passing
// bufs from a different Pool is undefined behavior.
func (p *Pool) Put(bufs [][]byte) {
	if len(bufs) == 0 {
		panic("row: Put called with empty bufs")
	}
	b := slabFromBuf(bufs[0])
	if b == nil {
		panic("row: Put called with empty buffer")
	}

	b.bucket.put(b)
}

// Slabs returns the total count of live slabs (in-use plus free)
// across all buckets. For tests and observability; locks each bucket
// briefly.
func (p *Pool) Slabs() int {
	n := 0
	for i := range p.buckets {
		bk := &p.buckets[i]
		bk.Lock()
		n += len(bk.used) + len(bk.free)
		bk.Unlock()
	}
	return n
}

// pop walks size's exact bucket up through the slack window and tries
// to recycle a free slab. Returns (bk, slab) for the first hit, or
// (exact bucket, nil) on a clean miss for the caller's slow-path
// [bucket.new].
func (p *Pool) pop(size int) (*bucket, *slab) {
	sizeIdx := size/rowSizeAlign - 1
	maxIdx := min(sizeIdx+slack, len(p.buckets)-1)
	for i := sizeIdx; i <= maxIdx; i++ {
		bk := &p.buckets[i]
		if s := bk.pop(); s != nil {
			return bk, s
		}
	}
	return &p.buckets[sizeIdx], nil
}

// new allocates a fresh slab at bk's exact key and adopts it directly
// into bk.used. alloc runs outside the lock — a multi-MiB mmap would
// dominate Get p95 if held across it.
func (bk *bucket) new(dataSize int) *slab {
	region, free := alloc(headerSize + dataSize)

	bk.Lock()
	defer bk.Unlock()

	s := &slab{
		bucket: bk,
		region: region,
		free:   free,
	}
	writeSlabPtr(region, s)
	bk.use(s)
	return s
}

// put hands b back to bk. The bk.used[b.usedIdx] == b identity check
// catches double-free and stale references in one shot. Arms the idle
// timer when used goes empty.
func (bk *bucket) put(b *slab) {
	bk.Lock()
	defer bk.Unlock()

	if b.usedIdx >= len(bk.used) || bk.used[b.usedIdx] != b {
		panic("row: Put called with released or already-freed slab")
	}

	// swap-delete from bk.used.
	last := len(bk.used) - 1
	if b.usedIdx != last {
		moved := bk.used[last]
		bk.used[b.usedIdx] = moved
		moved.usedIdx = b.usedIdx
	}
	bk.used[last] = nil
	bk.used = bk.used[:last]

	b.lastPut = bk.gen
	bk.free = append(bk.free, b)

	if len(bk.used) == 0 {
		bk.armIdle()
	}
}

// pop dequeues the most recently freed slab (LIFO) and adopts it into
// bk.used. Returns nil if bk.free is empty.
func (bk *bucket) pop() *slab {
	bk.Lock()
	defer bk.Unlock()

	n := len(bk.free)
	if n == 0 {
		return nil
	}
	s := bk.free[n-1]
	bk.free[n-1] = nil
	bk.free = bk.free[:n-1]

	bk.use(s)
	return s
}

// use cancels the idle timer, advances generation, runs aged eviction,
// and links s into bk.used. Caller holds bk.mu. Invoked from both the
// recycle path ([bucket.pop]) and the fresh-alloc path ([bucket.new]).
func (bk *bucket) use(s *slab) {
	bk.cancelIdle()
	bk.gen++
	bk.evict()

	s.usedIdx = len(bk.used)
	bk.used = append(bk.used, s)
}

// evict drops the oldest free slab if it has aged past
// evictionThreshold. At most one per call. Caller holds bk.mu.
func (bk *bucket) evict() {
	if len(bk.free) == 0 {
		return
	}

	oldest := bk.free[0]
	if bk.gen-oldest.lastPut < evictionThreshold {
		return
	}

	oldest.free(oldest.region)
	bk.free[0] = nil // drop the backing-array reference before reslicing
	bk.free = bk.free[1:]
}

// armIdle schedules a per-bucket drop after idleGrace. Lazily creates
// the timer on first arm and reuses via Reset afterwards. Reset is
// safe without a preceding Stop: cancelIdle/dropIdle leave the timer
// stopped or expired before any subsequent arm. Caller holds bk.mu.
func (bk *bucket) armIdle() {
	if bk.idleTimer == nil {
		bk.idleTimer = time.AfterFunc(idleGrace, bk.dropIdle)
		return
	}

	bk.idleTimer.Reset(idleGrace)
}

// cancelIdle stops the idle-drop timer if armed. Caller holds bk.mu.
func (bk *bucket) cancelIdle() {
	if bk.idleTimer != nil {
		bk.idleTimer.Stop()
	}
}

// dropIdle is the idle-timer callback: drops bk's free slabs if bk
// is still fully idle when it fires. Leaves idleTimer non-nil so the
// next armIdle reuses it via Reset.
func (bk *bucket) dropIdle() {
	bk.Lock()
	defer bk.Unlock()

	if len(bk.used) != 0 {
		return
	}

	for _, b := range bk.free {
		b.free(b.region)
	}
	bk.free = nil
}

// slab's backing region is laid out as
//
//	[ header (headerSize bytes) | data (rowSize * rowCount bytes) ]
//
// The first word of the header stores a back-pointer to this *slab so
// Put can recover it from any carved buffer by subtracting headerSize.
type slab struct {
	bucket *bucket

	region []byte       // full backing region including the header
	free   func([]byte) // releases region; mmapFree for off-heap, noopFree for Go-heap

	lastPut uint64
	usedIdx int // index in bucket.used; updated on swap-delete
}

// slabFromBuf recovers the owning *slab by reading the back-pointer
// stored headerSize bytes below the buffer's base. Symmetric with
// writeSlabPtr. A buffer whose backing array begins within headerSize
// of a page start can fault here — contract forbids passing such buffers.
func slabFromBuf(buf []byte) *slab {
	if len(buf) == 0 {
		return nil
	}
	base := unsafe.Pointer(unsafe.SliceData(buf))
	return *(**slab)(unsafe.Pointer(uintptr(base) - headerSize))
}

// writeSlabPtr stamps b's address into region's header so slabFromBuf
// can recover it from any buffer carved out of region[headerSize:].
func writeSlabPtr(region []byte, b *slab) {
	*(**slab)(unsafe.Pointer(unsafe.SliceData(region))) = b
}

// carve returns n contiguous []byte views of length size each into b's
// data region. Each slice is capacity-clamped so callers can't append
// past their row.
func (b *slab) carve(n, size int) [][]byte {
	bufs := make([][]byte, n)
	for i := range bufs {
		off := headerSize + i*size
		bufs[i] = b.region[off : off+size : off+size]
	}
	return bufs
}

// alloc returns a backing region and the matching release callback.
// Large allocations go through mmap (off-heap, invisible to GC) and
// pair with [mmapFree]; smaller ones use the SIMD-aligned Go-heap
// allocator and pair with noopFree (GC reclaims).
func alloc(size int) (data []byte, free func([]byte)) {
	if !disableMmap && size >= mmapThreshold {
		if d, err := mmapAlloc(size); err == nil {
			return d, mmapFree
		}
	}
	return reedsolomon.AllocAligned(1, size)[0], noopFree
}

func noopFree([]byte) {}

var _ reedsolomon.WorkAllocator = (*Pool)(nil)
