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
// once bucket.generation exceeds its lastFreeGen by evictionThreshold.
// When the pool goes fully idle (no in-use batches), an idle timer arms
// and drops every free batch after idleGrace if nothing arrives.
//
// Contract: buffers from [Pool.Get] must be returned together via
// [Pool.Put]; callers must not re-slice or split across calls. Put
// validates via a back-pointer embedded in each batch's region and
// panics on any violation — double-free, foreign pointer, or a pointer
// to a batch that has already been released. A pointer whose backing
// array lies near the start of a page can fault when Put reads the
// header below it, so callers must not pass buffers this pool never
// allocated.
package row

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/klauspost/reedsolomon"
)

// Structural constants — invariants of the layout, not tuning knobs.
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

	// idleGrace: wait after full idle before every free batch is dropped.
	idleGrace = 30 * time.Second

	// mmapThreshold: minimum batch size routed through mmap instead of
	// the Go heap. Above this, allocations stay invisible to GOGC's
	// heap-doubling pacer so the codec's multi-MiB work buffers don't
	// trigger OOMs.
	mmapThreshold = 1 << 20
)

// Pool is a bucketed allocator of fixed-shape row batches.
//
// Buckets live in a single flat slots slice laid out as len(rowCounts)
// partitions of (maxRowSize/rowSizeAlign) entries each. A partition is
// identified by a rowCount (rowCounts[partIdx]); within a partition,
// slot i is the bucket for rowSize (i+1)*64. Exact lookup and upward
// fallback are contiguous pointer reads.
//
// Pool achieves 0(1) for both Put and Get.
//
// Safe for concurrent use.
type Pool struct {
	mu sync.Mutex

	maxRowSize int
	rowCounts  []int // accepted row counts; fixed at New()

	// slots[partIdx*(maxRowSize/rowSizeAlign) + sizeIdx] holds the bucket
	// for (rowCounts[partIdx], (sizeIdx+1)*rowSizeAlign).
	slots []*bucket

	// batches anchors every live *batch on the Go heap — a batch's header
	// inside its (possibly mmap'd) backing region is invisible to GC, so
	// we must hold a Go-heap reference here for as long as the batch lives.
	// Swap-delete on evict via batch.poolIdx keeps removal O(1).
	batches []*batch

	inUse     int
	idleTimer *time.Timer
}

// bucket holds free batches for one (rowCount, rowSize) pair and its
// generation counter. free is a FIFO by release time: Put appends; reuse
// pops the back (LIFO warmth); aged eviction pops the front (oldest first).
type bucket struct {
	rowSize, rowCount int
	generation        uint64
	free              []*batch
}

// batch's backing region is laid out as
//
//	[ header (headerSize bytes) | data (rowSize * rowCount bytes) ]
//
// The first word of the header stores a back-pointer to this *batch so
// Put recovers the batch from the caller's first carved buffer by
// subtracting headerSize and loading a pointer. The data slice is not
// stored on the batch — it's computed on demand as
// region[headerSize : headerSize + bucket.rowSize*bucket.rowCount],
// keeping the struct in a single cache line.
type batch struct {
	bucket      *bucket
	region      []byte // full backing region including the header
	mmapped     bool
	inUse       bool
	lastFreeGen uint64
	poolIdx     int // index in pool.batches; updated on swap-delete
}

// New creates a Pool accepting the given row count classes. At least one
// rowCount must be supplied; each must be positive and distinct values
// get separate partitions (duplicates are harmless but waste a slot
// table). maxRowSize must be a positive multiple of 64. The pool is
// deliberately codec-agnostic: callers decide which row counts it sees
// (e.g. [AssemblerBatchRows] and ProtocolParams.CodecWorkRows).
func New(maxRowSize int, rowCounts ...int) *Pool {
	if maxRowSize < rowSizeAlign || maxRowSize%rowSizeAlign != 0 {
		panic(fmt.Sprintf("row: maxRowSize %d must be a positive multiple of %d", maxRowSize, rowSizeAlign))
	}
	if len(rowCounts) == 0 {
		panic("row: at least one rowCount must be supplied")
	}
	for _, rc := range rowCounts {
		if rc <= 0 {
			panic(fmt.Sprintf("row: rowCount %d must be positive", rc))
		}
	}
	return &Pool{
		maxRowSize: maxRowSize,
		rowCounts:  append([]int(nil), rowCounts...),
		slots:      make([]*bucket, len(rowCounts)*(maxRowSize/rowSizeAlign)),
	}
}

// Get returns n buffers of size bytes each, carved from one batch.
// size must be a positive multiple of 64 in [64, maxRowSize] and n must
// be one of the row counts this pool was built for; violations panic.
// Implements [reedsolomon.WorkAllocator].
func (p *Pool) Get(n, size int) [][]byte {
	if n == 0 {
		return nil
	}
	if size < rowSizeAlign || size > p.maxRowSize || size%rowSizeAlign != 0 {
		panic(fmt.Sprintf("row: size %d invalid (want positive multiple of %d, <= %d)", size, rowSizeAlign, p.maxRowSize))
	}
	partIdx := p.partition(n)
	if partIdx < 0 {
		panic(fmt.Sprintf("row: row count %d not accepted by this pool", n))
	}

	bk, b := p.tryAcquire(partIdx, n, size)
	if b == nil {
		// mmap outside the lock
		region, mmapped := allocBacking(headerSize + bk.rowSize*bk.rowCount)
		b = p.install(bk, region, mmapped)
	}

	return carve(b.region[headerSize:headerSize+n*size], n, size)
}

// tryAcquire prepares the bucket (creating it
// if needed), advances its generation, runs aged eviction, and tries to
// hand out a free batch via exact match or upward-slack fallback.
// Returns the bucket plus either a ready-to-use batch, or nil if the
// caller must allocate a new batch at the bucket's key.
func (p *Pool) tryAcquire(partIdx, n, size int) (*bucket, *batch) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.cancelIdleTimer()

	partBase := partIdx * (p.maxRowSize / rowSizeAlign)
	reqIdx := size/rowSizeAlign - 1
	slotIdx := partBase + reqIdx
	bk := p.slots[slotIdx]
	if bk == nil {
		bk = &bucket{rowSize: size, rowCount: n}
		p.slots[slotIdx] = bk
	}
	bk.generation++
	p.evict(bk)

	b := popFree(bk)
	if b == nil {
		b = p.fallback(partBase, reqIdx)
	}
	if b != nil {
		b.inUse = true
		p.inUse++
	}
	return bk, b
}

// install wraps a freshly-allocated region into a batch bound to bk,
// anchors it in p.batches, and marks it in-use. Cancels any idle-drop
// timer a concurrent Put armed while the caller was mmap'ing.
func (p *Pool) install(bk *bucket, region []byte, mmapped bool) *batch {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.cancelIdleTimer()

	b := &batch{
		bucket:  bk,
		region:  region,
		mmapped: mmapped,
		inUse:   true,
		poolIdx: len(p.batches),
	}
	*(**batch)(unsafe.Pointer(unsafe.SliceData(region))) = b
	p.batches = append(p.batches, b)
	p.inUse++
	return b
}

// cancelIdleTimer stops and clears the idle-drop timer if armed. Caller
// holds p.mu.
func (p *Pool) cancelIdleTimer() {
	if p.idleTimer != nil {
		p.idleTimer.Stop()
		p.idleTimer = nil
	}
}

// Put returns the batch backing bufs to its owning bucket. Panics on
// any contract violation — empty bufs, a pointer that doesn't resolve
// to a live batch (foreign or released), or a double-free.
func (p *Pool) Put(bufs [][]byte) {
	if len(bufs) == 0 {
		panic("row: Put called with empty bufs")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	b := batchFromBuf(bufs[0])
	if b == nil {
		panic("row: Put called with empty buffer")
	}
	if b.poolIdx >= len(p.batches) || p.batches[b.poolIdx] != b {
		panic("row: Put called with foreign or released batch")
	}
	if !b.inUse {
		panic("row: double free")
	}
	b.inUse = false
	b.lastFreeGen = b.bucket.generation
	b.bucket.free = append(b.bucket.free, b)
	p.inUse--

	if p.inUse == 0 {
		p.idleTimer = time.AfterFunc(idleGrace, p.dropIdle)
	}
}

// Batches returns the total count of live batches. For tests and
// observability; takes the pool lock.
func (p *Pool) Batches() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.batches)
}

// partition returns the partition index for rowCount, or -1 if
// rowCount is not one of this pool's accepted classes. rowCounts is
// fixed at New() so this scan (≤3 entries in practice) is lock-free.
func (p *Pool) partition(rowCount int) int {
	for i, rc := range p.rowCounts {
		if rc == rowCount {
			return i
		}
	}
	return -1
}

// fallback walks up to slack slots above reqIdx within the current
// partition and returns a popped free batch from the first larger-class
// bucket that has one. Consuming from a larger bucket advances its
// generation and runs its aged-eviction, so its other free batches age
// on consumption just as they would under a direct Get.
func (p *Pool) fallback(partBase, reqIdx int) *batch {
	maxIdx := min(reqIdx+slack, p.maxRowSize/rowSizeAlign-1)
	for i := reqIdx + 1; i <= maxIdx; i++ {
		bk := p.slots[partBase+i]
		if bk == nil {
			continue
		}
		if b := popFree(bk); b != nil {
			bk.generation++
			p.evict(bk)
			return b
		}
	}
	return nil
}

// evict drops the oldest free batch from bk if it has aged
// past evictionThreshold. bk.free is FIFO so the head is always the
// oldest candidate; generations advance by one per Get, so at most one
// batch transitions to evictable per call.
func (p *Pool) evict(bk *bucket) {
	if len(bk.free) == 0 {
		return
	}
	oldest := bk.free[0]
	if bk.generation-oldest.lastFreeGen < evictionThreshold {
		return
	}
	p.release(oldest)
	bk.free[0] = nil // drop the reference in the backing array before reslicing
	bk.free = bk.free[1:]
}

// release drops a batch's backing memory and unlinks it from
// p.batches via swap-delete.
func (p *Pool) release(b *batch) {
	last := len(p.batches) - 1
	if b.poolIdx != last {
		moved := p.batches[last]
		p.batches[b.poolIdx] = moved
		moved.poolIdx = b.poolIdx
	}
	p.batches[last] = nil
	p.batches = p.batches[:last]
	if b.mmapped {
		_ = mmapFree(b.region)
	}
}

// dropIdle is the idle-timer callback: if the pool is still fully
// idle, drop every free batch; otherwise no-op.
func (p *Pool) dropIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.idleTimer = nil
	if p.inUse != 0 {
		return
	}
	for _, bk := range p.slots {
		if bk == nil {
			continue
		}
		for _, b := range bk.free {
			p.release(b)
		}
		bk.free = nil
	}
}

// popFree pops the most recently freed batch from bk, or returns nil.
func popFree(bk *bucket) *batch {
	n := len(bk.free)
	if n == 0 {
		return nil
	}
	b := bk.free[n-1]
	bk.free[n-1] = nil
	bk.free = bk.free[:n-1]
	return b
}

// batchFromBuf recovers the owning *batch from a buffer carved by this
// pool, via the back-pointer stored headerSize bytes below the buffer's
// base. The caller (Put) validates the returned *batch against p.batches
// before trusting it, so a wrong-but-readable pointer is caught there.
// A pointer whose backing array begins within headerSize of the start of
// a page can fault here — contract forbids passing such buffers.
func batchFromBuf(buf []byte) *batch {
	if len(buf) == 0 {
		return nil
	}
	base := unsafe.Pointer(unsafe.SliceData(buf))
	return *(**batch)(unsafe.Pointer(uintptr(base) - headerSize))
}

// carve slices data into n equal-sized buffers using full-slice expressions
// so sub-buffers can't append past their length into neighbors.
func carve(data []byte, n, size int) [][]byte {
	bufs := make([][]byte, n)
	for i := range n {
		start, end := i*size, (i+1)*size
		bufs[i] = data[start:end:end]
	}
	return bufs
}

// allocBacking returns aligned backing bytes for a batch. Large batches
// go through mmap (off-heap); smaller ones through the SIMD-aligned
// Go-heap allocator.
func allocBacking(size int) (data []byte, mmapped bool) {
	if !disableMmap && size >= mmapThreshold {
		if d, err := mmapAlloc(size); err == nil {
			return d, true
		}
	}
	return reedsolomon.AllocAligned(1, size)[0], false
}

var _ reedsolomon.WorkAllocator = (*Pool)(nil)
