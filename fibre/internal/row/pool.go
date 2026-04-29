// Package row provides a bucketed allocator of fixed-shape row batches.
//
// A batch is the indivisible allocation unit: rowCount rows of rowSize
// bytes each. A Get(rowCount, rowSize) is served from the exact bucket
// or from a larger-rowSize bucket within a slack window on the same
// rowCount — never smaller. Failing both, a new batch is allocated at
// the exact requested key.
//
// Retention is demand-driven: each bucket has a generation counter that
// advances on every Get to that bucket. A free batch becomes evictable
// once bucket.gen exceeds its lastPut by evictionThreshold. When a
// bucket goes fully idle (no in-use batches), an independent per-bucket
// idle timer arms and drops that bucket's free batches after idleGrace
// if no Get arrives.
//
// Contract: buffers from [Pool.Get] must be returned together via
// [Pool.Put] to the same Pool that issued them; callers must not
// re-slice or split across calls. Put validates a back-pointer embedded
// in each batch's region and panics on double-free or already-released
// batches. Passing buffers from a different Pool instance is undefined
// behavior. A pointer whose backing array lies near the start of a page
// can fault when Put reads the header below it, so callers must not
// pass buffers this pool never allocated.
package row

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/klauspost/reedsolomon"
)

// Structural constants — invariants of the memory layout, not tuning knobs.
const (
	// rowSizeAlign is the quantum for both rowSize and the minimum
	// accepted rowSize; SIMD-aligned to 64 bytes.
	rowSizeAlign = 64

	// headerSize prefixes every batch's backing region. Sized to keep
	// data 64-byte (SIMD) aligned; the first word is a back-pointer to
	// the owning *batch, used by Put to recover it from a carved buffer.
	headerSize = 64
)

// Pool tunables — safe to adjust, governed by workload characteristics.
const (
	// slack: max number of rowSizeAlign slot steps above the exact match
	// a free batch may occupy for upward fallback. Waste per reused batch
	// is slack*rowSizeAlign*rowCount bytes — constant per row, so safe to
	// apply at any rowSize. With defaults and Celestia's shape: ≤1.5 MiB
	// on the assembler batch, ≤4 MiB on the codec-work batch.
	slack = 2

	// evictionThreshold: generation ticks a free batch must age through
	// before becoming evictable.
	evictionThreshold = 4

	// idleGrace: wait after a bucket goes fully idle before its free
	// batches are dropped.
	idleGrace = 30 * time.Second

	// mmapThreshold: minimum batch size routed through mmap instead of
	// the Go heap. Above this, allocations stay invisible to GOGC's
	// heap-doubling pacer so the codec's multi-MiB work buffers don't
	// trigger OOMs.
	mmapThreshold = 1 << 20 // 1 MiB
)

// Pool is a bucketed allocator of fixed-shape row batches for a single
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

// bucket holds free and in-use batches for one (rowCount, rowSize)
// pair. Every state mutation is serialized by the embedded mutex.
//
// free is a FIFO by release time: reuse pops the back (LIFO warmth),
// aged eviction pops the front (oldest first). used anchors live
// in-use batches on the Go heap — the header back-pointer inside a
// (possibly mmap'd) region is invisible to GC, so a Go-heap reference
// must outlive the batch. idleTimer arms when used goes empty and
// drops free batches after idleGrace if no Get arrives.
type bucket struct {
	sync.Mutex

	gen        uint64
	free, used []*batch
	idleTimer  *time.Timer

	// freeLen mirrors len(free) for an unsynchronized peek in [Pool.find].
	freeLen atomic.Int32
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

// Get returns n buffers of size bytes each, carved from one batch.
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

	// find → maybe new → pop, retrying on race-loss. a failed pop means
	// a peer Get drained the chosen bucket between our peek and take;
	// re-finding picks up free batches from any slack-window bucket on
	// retry. bounded by concurrent contention.
	for {
		bk, ok := p.find(size)
		if !ok {
			bk.new(n * size)
		}
		b := bk.pop()
		if b == nil {
			continue
		}
		return b.carve(n, size)
	}
}

// Put returns the batch backing bufs to its owning bucket. Panics on
// empty bufs, an already-released batch, or a double-free. Passing
// bufs from a different Pool is undefined behavior.
func (p *Pool) Put(bufs [][]byte) {
	if len(bufs) == 0 {
		panic("row: Put called with empty bufs")
	}
	b := batchFromBuf(bufs[0])
	if b == nil {
		panic("row: Put called with empty buffer")
	}

	b.bucket.put(b)
}

// Batches returns the total count of live batches (in-use plus free)
// across all buckets. For tests and observability; locks each bucket
// briefly.
func (p *Pool) Batches() int {
	n := 0
	for i := range p.buckets {
		bk := &p.buckets[i]
		bk.Lock()
		n += len(bk.used) + len(bk.free)
		bk.Unlock()
	}
	return n
}

// find walks from size's exact bucket up through the slack window and
// returns (bk, true) for the first with freeLen > 0. The atomic peek
// skips empty buckets without locking — stale reads are benign. On a
// clean miss returns (exact bucket, false) for the caller's slow-path
// new.
func (p *Pool) find(size int) (*bucket, bool) {
	sizeIdx := size/rowSizeAlign - 1
	maxIdx := min(sizeIdx+slack, len(p.buckets)-1)
	for i := sizeIdx; i <= maxIdx; i++ {
		bk := &p.buckets[i]
		if bk.freeLen.Load() > 0 {
			return bk, true
		}
	}
	return &p.buckets[sizeIdx], false
}

// new allocates a fresh batch and parks it on bk's free queue for the
// caller's follow-up [bucket.pop]. alloc runs outside the lock — a
// multi-MiB mmap would dominate Get p95 if held across it. Concurrent
// slow paths each install their own batch; the surplus self-heals via
// aged eviction.
func (bk *bucket) new(dataSize int) {
	region, mmapped := alloc(headerSize + dataSize)

	bk.Lock()
	defer bk.Unlock()

	b := &batch{
		bucket:  bk,
		region:  region,
		mmapped: mmapped,
		lastPut: bk.gen, // anchor against premature aged eviction
	}
	writeBatchPtr(region, b)
	bk.cancelIdle() // a Get is in flight; bk is not idle anymore
	bk.free = append(bk.free, b)
	bk.freeLen.Store(int32(len(bk.free)))
}

// put hands b back to bk. The bk.used[b.usedIdx] == b identity check
// catches double-free and stale references in one shot. Arms the idle
// timer when used goes empty.
func (bk *bucket) put(b *batch) {
	bk.Lock()
	defer bk.Unlock()

	if b.usedIdx >= len(bk.used) || bk.used[b.usedIdx] != b {
		panic("row: Put called with released or already-freed batch")
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
	bk.freeLen.Store(int32(len(bk.free)))

	if len(bk.used) == 0 {
		bk.armIdle()
	}
}

// pop is the sole path by which a batch transitions from free to used.
// Pops the most recently freed batch (LIFO), cancels the idle timer,
// advances generation, runs aged eviction, and links the batch into
// bk.used — all under one lock. Returns nil if bk.free is empty
// (caller's freeLen peek was stale, or a peer raced ahead).
func (bk *bucket) pop() *batch {
	bk.Lock()
	defer bk.Unlock()

	n := len(bk.free)
	if n == 0 {
		return nil
	}
	b := bk.free[n-1]
	bk.free[n-1] = nil
	bk.free = bk.free[:n-1]
	bk.freeLen.Store(int32(n - 1))

	bk.cancelIdle()
	bk.gen++
	bk.evict()

	b.usedIdx = len(bk.used)
	bk.used = append(bk.used, b)
	return b
}

// evict drops the oldest free batch if it has aged past
// evictionThreshold. At most one per call. Caller holds bk.mu.
func (bk *bucket) evict() {
	if len(bk.free) == 0 {
		return
	}
	oldest := bk.free[0]
	if bk.gen-oldest.lastPut < evictionThreshold {
		return
	}
	if oldest.mmapped {
		_ = mmapFree(oldest.region)
	}
	bk.free[0] = nil // drop the backing-array reference before reslicing
	bk.free = bk.free[1:]
	bk.freeLen.Store(int32(len(bk.free)))
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

// dropIdle is the idle-timer callback: drops bk's free batches if bk
// is still fully idle when it fires. Leaves idleTimer non-nil so the
// next armIdle reuses it via Reset.
func (bk *bucket) dropIdle() {
	bk.Lock()
	defer bk.Unlock()
	if len(bk.used) != 0 {
		return
	}
	for _, b := range bk.free {
		if b.mmapped {
			_ = mmapFree(b.region)
		}
	}
	bk.free = nil
	bk.freeLen.Store(0)
}

// batch's backing region is laid out as
//
//	[ header (headerSize bytes) | data (rowSize * rowCount bytes) ]
//
// The first word of the header stores a back-pointer to this *batch so
// Put can recover it from any carved buffer by subtracting headerSize.
type batch struct {
	bucket *bucket

	region  []byte // full backing region including the header
	mmapped bool

	lastPut uint64
	usedIdx int // index in bucket.used; updated on swap-delete
}

// batchFromBuf recovers the owning *batch by reading the back-pointer
// stored headerSize bytes below the buffer's base. Symmetric with
// writeBatchPtr. A buffer whose backing array begins within headerSize
// of a page start can fault here — contract forbids passing such buffers.
func batchFromBuf(buf []byte) *batch {
	if len(buf) == 0 {
		return nil
	}
	base := unsafe.Pointer(unsafe.SliceData(buf))
	return *(**batch)(unsafe.Pointer(uintptr(base) - headerSize))
}

// writeBatchPtr stamps b's address into region's header so batchFromBuf
// can recover it from any buffer carved out of region[headerSize:].
func writeBatchPtr(region []byte, b *batch) {
	*(**batch)(unsafe.Pointer(unsafe.SliceData(region))) = b
}

// carve returns n contiguous []byte views of length size each into b's
// data region. Each slice is capacity-clamped so callers can't append
// past their row.
func (b *batch) carve(n, size int) [][]byte {
	bufs := make([][]byte, n)
	for i := range bufs {
		off := headerSize + i*size
		bufs[i] = b.region[off : off+size : off+size]
	}
	return bufs
}

// alloc returns aligned backing bytes for a batch. Large allocations
// go through mmap (off-heap, invisible to GC); smaller ones use the
// SIMD-aligned Go-heap allocator.
func alloc(size int) (data []byte, mmapped bool) {
	if !disableMmap && size >= mmapThreshold {
		if d, err := mmapAlloc(size); err == nil {
			return d, true
		}
	}
	return reedsolomon.AllocAligned(1, size)[0], false
}

var _ reedsolomon.WorkAllocator = (*Pool)(nil)
