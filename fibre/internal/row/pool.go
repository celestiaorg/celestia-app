// Package row allocates row buffers for the Fibre codec.
//
// Each Get returns one slab, a contiguous region carved into rows. Slabs of
// 1 MiB or more are mmap'd (off the Go heap, advised for transparent huge
// pages); smaller ones come from the Go heap.
//
// Returned slabs go to one process-wide free list, so a steady stream of blobs
// reuses memory that is already faulted in instead of having the kernel zero
// hundreds of MiB of fresh pages per blob. The list is bounded three ways:
//
//   - a Get reuses the smallest free slab that fits and is at most twice the
//     request, so a small request never pins a much larger slab;
//   - free slabs total at most maxFreeBytes, oldest released first;
//   - a free slab unused for idleTTL is released.
//
// Contract: return every slab exactly once, to any Pool, by passing the
// buffers Get returned (Put) or the region GetRegion returned (PutRegion).
// Put panics on a buffer that is not an in-use slab, such as a double free.
package row

import (
	"fmt"
	"slices"
	"sync"
	"time"
	"unsafe"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/field"
	"github.com/klauspost/reedsolomon"
)

const (
	// rowSizeAlign is the row size quantum: the Leopard chunk size the SIMD
	// GF(2^16) kernels require, which also keeps every carved row 64-byte
	// aligned.
	rowSizeAlign = field.LeopardChunkSize

	// maxWaste bounds how much larger than the request a reused slab may be.
	maxWaste = 2

	// mmapThreshold is the smallest slab mmap'd instead of taken from the Go
	// heap, where GOGC's pacer would count the codec's large buffers twice.
	mmapThreshold = 1 << 20

	// maxFreeBytes caps the memory held in free slabs, a few codec slabs. A
	// miss only costs a fresh mapping (page faults and kernel zeroing, ~25 µs
	// per MiB with huge pages on Graviton4), while a larger cap carries one
	// burst's peak into the next. 1 GiB cost 1-2% throughput in CPU-bound
	// gateway load; 2 GiB did not.
	maxFreeBytes = 2 << 30

	// idleTTL is how long a free slab is kept without reuse. Under load a
	// slab is reused within milliseconds; one idle this long is surplus.
	idleTTL = 2 * time.Second
)

// slabs is the free list every Pool shares, so memory freed by one stage
// (parity, codec work, downloads) can serve another and the bounds above hold
// for the process as a whole.
var slabs = newAllocator()

// Pool hands out slabs of rowCount rows of up to maxRowSize bytes. It only
// checks request shapes; storage comes from the shared free list.
//
// Safe for concurrent use. Implements [reedsolomon.WorkAllocator].
type Pool struct {
	rowCount   int
	maxRowSize int
	a          *allocator
}

// NewPool returns a Pool for rowCount rows of up to maxRowSize bytes.
// maxRowSize must be a positive multiple of 64; rowCount must be positive.
func NewPool(maxRowSize, rowCount int) *Pool {
	if maxRowSize < rowSizeAlign || maxRowSize%rowSizeAlign != 0 {
		panic(fmt.Sprintf("row: maxRowSize %d must be a positive multiple of %d", maxRowSize, rowSizeAlign))
	}
	if rowCount <= 0 {
		panic(fmt.Sprintf("row: rowCount %d must be positive", rowCount))
	}
	return &Pool{rowCount: rowCount, maxRowSize: maxRowSize, a: slabs}
}

// Get returns n rows of size bytes each, carved from one slab. n must equal
// the pool's rowCount; size must be a positive multiple of 64 no larger than
// maxRowSize. Violations panic. Each row's capacity is clamped to its length.
func (p *Pool) Get(n, size int) [][]byte {
	region := p.GetRegion(n, size)
	rows := make([][]byte, n)
	for i := range rows {
		rows[i] = region[i*size : (i+1)*size : (i+1)*size]
	}
	return rows
}

// GetRegion returns the n*size contiguous bytes of one slab, without the
// per-row view that [Pool.Get] builds. Release it with [Pool.PutRegion].
func (p *Pool) GetRegion(n, size int) []byte {
	if n != p.rowCount {
		panic(fmt.Sprintf("row: rowCount %d does not match pool's %d", n, p.rowCount))
	}
	if size < rowSizeAlign || size > p.maxRowSize || size%rowSizeAlign != 0 {
		panic(fmt.Sprintf("row: size %d invalid (want positive multiple of %d, <= %d)", size, rowSizeAlign, p.maxRowSize))
	}
	return p.a.get(n * size)
}

// Put returns the slab behind rows, as returned by [Pool.Get].
func (p *Pool) Put(rows [][]byte) {
	if len(rows) == 0 {
		panic("row: Put called with empty bufs")
	}
	p.a.put(rows[0])
}

// PutRegion returns the slab behind region, as returned by [Pool.GetRegion].
func (p *Pool) PutRegion(region []byte) {
	p.a.put(region)
}

// allocator is the free list behind every Pool.
type allocator struct {
	mu sync.Mutex
	// inUse maps a slab's first byte to the whole slab: callers see only the
	// size they asked for, and Put must reject anything else (double frees).
	inUse     map[*byte][]byte
	free      []freeSlab // oldest first
	freeBytes int
	trimmer   sync.Once

	maxFree int           // maxFreeBytes outside tests
	ttl     time.Duration // idleTTL outside tests
}

type freeSlab struct {
	buf   []byte
	since time.Time
}

func newAllocator() *allocator {
	return &allocator{inUse: make(map[*byte][]byte), maxFree: maxFreeBytes, ttl: idleTTL}
}

// get returns size bytes from the smallest free slab that fits within
// maxWaste, or from a new slab.
func (a *allocator) get(size int) []byte {
	a.mu.Lock()
	best := -1
	for i, f := range a.free {
		if c := len(f.buf); c >= size && c <= maxWaste*size && (best < 0 || c <= len(a.free[best].buf)) {
			best = i // <= prefers the most recently freed among equal sizes
		}
	}
	var buf []byte
	if best >= 0 {
		buf = a.free[best].buf
		a.free = slices.Delete(a.free, best, best+1)
		a.freeBytes -= len(buf)
		a.inUse[unsafe.SliceData(buf)] = buf
	}
	a.mu.Unlock()

	if buf == nil {
		buf = alloc(size) // outside the lock: mapping MiBs is slow
		a.mu.Lock()
		a.inUse[unsafe.SliceData(buf)] = buf
		a.mu.Unlock()
	}
	return buf[:size:size]
}

// put moves the slab starting at buf to the free list and releases whatever
// the bounds no longer allow.
func (a *allocator) put(buf []byte) {
	if cap(buf) == 0 {
		panic("row: Put called with empty buffer")
	}
	key := unsafe.SliceData(buf)
	now := time.Now()

	a.mu.Lock()
	slab, ok := a.inUse[key]
	if !ok {
		a.mu.Unlock()
		panic("row: Put called with a buffer that is not an in-use slab (double free?)")
	}
	delete(a.inUse, key)
	a.free = append(a.free, freeSlab{slab, now})
	a.freeBytes += len(slab)
	expired := a.trimLocked(now)
	a.mu.Unlock()

	release(expired)
	a.trimmer.Do(func() { go a.trimLoop() })
}

// trimLoop releases idle slabs when no Put arrives to do it.
func (a *allocator) trimLoop() {
	for now := range time.Tick(a.ttl / 2) {
		a.mu.Lock()
		expired := a.trimLocked(now)
		a.mu.Unlock()
		release(expired)
	}
}

// trimLocked removes free slabs idle for a.ttl or over a.maxFree bytes,
// oldest first, and returns them for release outside the lock.
func (a *allocator) trimLocked(now time.Time) [][]byte {
	n := 0
	for n < len(a.free) && (a.freeBytes > a.maxFree || now.Sub(a.free[n].since) >= a.ttl) {
		a.freeBytes -= len(a.free[n].buf)
		n++
	}
	if n == 0 {
		return nil
	}
	expired := make([][]byte, n)
	for i := range expired {
		expired[i] = a.free[i].buf
	}
	a.free = slices.Delete(a.free, 0, n)
	return expired
}

// alloc returns a new 64-byte aligned slab of at least size bytes: mmap'd
// at or above mmapThreshold, from the Go heap below it.
func alloc(size int) []byte {
	if !disableMmap && size >= mmapThreshold {
		buf, err := mmapAlloc(size)
		if err != nil {
			panic(err) // like a failed Go heap allocation, which is fatal
		}
		return buf
	}
	return reedsolomon.AllocAligned(1, size)[0]
}

// release returns mmap'd slabs to the OS; Go-heap slabs are left to the GC.
func release(bufs [][]byte) {
	for _, buf := range bufs {
		if !disableMmap && len(buf) >= mmapThreshold {
			mmapFree(buf)
		}
	}
}

var _ reedsolomon.WorkAllocator = (*Pool)(nil)
