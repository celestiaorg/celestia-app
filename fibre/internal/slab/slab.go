// Package slab provides a growable byte-region allocator backed by
// SIMD-aligned slabs.
//
// It is used by fibre to replace sync.Pool for multi-hundred-MiB Reed-Solomon
// work and parity buffers, which sync.Pool drops aggressively under GC pressure.
//
// Contract: buffers returned by [Pool.Get] must be passed back to [Pool.Put]
// exactly as received — callers must not re-slice. The allocator applies
// cheap pointer-in-slab + overshoot bounds checks but does not verify exact
// allocation boundaries, so a re-sliced Put would corrupt the free list.
// All internal callers comply; do not expose this package to untrusted code.
package slab

import (
	"cmp"
	"slices"
	"sync"
	"unsafe"

	"github.com/klauspost/reedsolomon"
)

// freeSlabShrinkGrace is the number of terminal Shrink calls a fully-free
// slab may survive while the pool is still active. This adds hysteresis for
// overlap-driven slab churn without pinning burst capacity after the pool
// goes fully idle.
const freeSlabShrinkGrace = 2

// Pool is a growable allocator of SIMD-aligned byte regions. Slabs are
// grown on demand, sized to the unsatisfied tail of each request, and
// dropped by [Pool.Shrink] once they've been idle long enough. Retention
// is purely demand-driven: hot slabs stay alive via the grace-period
// counter (reset on each alloc); cold ones get evicted once the pool
// goes fully idle. Implements [reedsolomon.WorkAllocator]. Safe for
// concurrent use.
type Pool struct {
	mu    sync.Mutex
	slabs []*slab

	// lifetime counters, under mu
	allocs int64 // number of times Get grew a new slab
	evicts int64 // number of times Shrink dropped a slab
}

// New creates an empty Pool.
func New() *Pool {
	return &Pool{slabs: make([]*slab, 0)}
}

// Get returns n buffers of size bytes each, drawn from free regions across
// existing slabs. Grows new slabs as needed to satisfy the request.
//
// Implements [reedsolomon.WorkAllocator].
func (p *Pool) Get(n, size int) [][]byte {
	if n == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	bufs := make([][]byte, 0, n)
	for _, s := range p.slabs {
		bufs = s.allocMany(bufs, n-len(bufs), size)
		if len(bufs) == n {
			return bufs
		}
	}

	// grow by the remaining deficit after reusing whatever existing slabs
	// could already satisfy.
	remaining := n - len(bufs)
	s := newSlab(remaining * size)
	p.slabs = append(p.slabs, s)
	p.allocs++
	return s.allocMany(bufs, remaining, size)
}

// Put frees each buffer back to its owning slab. Put does NOT shrink the
// pool — slabs are retained for reuse. Call [Pool.Shrink] explicitly at
// lifecycle boundaries (e.g., after a blob upload completes).
//
// Implements [reedsolomon.WorkAllocator].
func (p *Pool) Put(buffers [][]byte) {
	if len(buffers) == 0 {
		return
	}
	// sort by data pointer: within one slab buffers land in ascending
	// offset order, letting freeBatch use a monotonic coalesce hint and
	// merge adjacent-in-memory buffers into single region insertions.
	slices.SortFunc(buffers, func(a, b []byte) int {
		return cmp.Compare(
			uintptr(unsafe.Pointer(unsafe.SliceData(a))),
			uintptr(unsafe.Pointer(unsafe.SliceData(b))),
		)
	})
	p.mu.Lock()
	defer p.mu.Unlock()

	// walk sorted buffers, grouping consecutive same-slab runs; freeBatch
	// then merges adjacent-in-memory regions within each run before coalescing.
	for i := 0; i < len(buffers); {
		var owner *slab
		for _, s := range p.slabs {
			if s.contains(buffers[i]) {
				owner = s
				break
			}
		}
		if owner == nil {
			i++
			continue
		}
		j := i + 1
		for j < len(buffers) && owner.contains(buffers[j]) {
			j++
		}
		owner.freeBatch(buffers[i:j])
		i = j
	}
}

// Shrink releases fully-free slabs smallest-first. While the pool is
// still active, a fully-free slab survives up to [freeSlabShrinkGrace]
// shrink cycles before becoming evictable — this absorbs brief free/reuse
// gaps during overlapping uploads. Once the pool goes fully idle all free
// slabs are dropped immediately.
func (p *Pool) Shrink() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// single pass: bump the idle counter on every free slab and note whether
	// any slab is still in use.
	fullyIdle := true
	for _, s := range p.slabs {
		if s.isFree() {
			s.idleShrinks++
		} else {
			fullyIdle = false
		}
	}

	for {
		smallest := -1
		for i, s := range p.slabs {
			if !s.isFree() {
				continue
			}
			if !fullyIdle && s.idleShrinks < freeSlabShrinkGrace {
				continue // still within grace period
			}
			if smallest == -1 || s.size < p.slabs[smallest].size {
				smallest = i
			}
		}
		if smallest == -1 {
			return
		}
		if p.slabs[smallest].mmapped {
			_ = mmapFree(p.slabs[smallest].data)
		}
		p.slabs = slices.Delete(p.slabs, smallest, smallest+1)
		p.evicts++
	}
}

// Slabs returns the current number of slabs. Intended for observability.
func (p *Pool) Slabs() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.slabs)
}

// Stats is a snapshot of pool capacity, fragmentation, and lifetime counters.
type Stats struct {
	Slabs          int   // number of slabs currently held
	TotalBytes     int64 // sum of slab sizes (reserved capacity)
	FreeBytes      int64 // sum of free regions across all slabs
	LargestFreeRun int64 // largest contiguous free region in any single slab
	Allocs         int64 // lifetime count of new-slab allocations
	Evictions      int64 // lifetime count of slabs evicted by Shrink
}

// Stats returns a snapshot of pool capacity, fragmentation, and lifetime
// counters. Intended for metrics/observability; takes the pool lock, so avoid
// calling it on the hot path.
func (p *Pool) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := Stats{
		Slabs:     len(p.slabs),
		Allocs:    p.allocs,
		Evictions: p.evicts,
	}
	for _, s := range p.slabs {
		free, maxRegion := s.stats()
		st.TotalBytes += int64(s.size)
		st.FreeBytes += int64(free)
		if int64(maxRegion) > st.LargestFreeRun {
			st.LargestFreeRun = int64(maxRegion)
		}
	}
	return st
}

// slab is a contiguous SIMD-aligned byte region with a first-fit free-list.
type slab struct {
	data    []byte
	size    int
	mmapped bool // true if data was allocated via mmap (not Go heap)
	regions []region

	// idleShrinks counts consecutive Shrink calls this slab has been fully
	// free. Reset to 0 on every successful alloc. Used by Shrink to apply a
	// grace period before evicting burst-era slabs; see freeSlabShrinkGrace.
	idleShrinks int
}

type region struct {
	off, size int
}

// mmapThreshold is the minimum slab size for mmap. Smaller slabs use the
// Go heap; larger ones use mmap so they stay invisible to GOGC's heap
// doubling.
const mmapThreshold = 1 << 20 // 1 MiB

func newSlab(size int) *slab {
	data, mmapped := allocBacking(size)
	return &slab{
		data:    data,
		size:    len(data),
		mmapped: mmapped,
		regions: []region{{0, len(data)}},
	}
}

// allocBacking returns an aligned byte slab of at least `size` bytes. Large
// requests go through mmap (off-heap) when not disabled; everything else
// goes through the SIMD-aligned Go-heap allocator.
func allocBacking(size int) (data []byte, mmapped bool) {
	if !disableMmap && size >= mmapThreshold {
		if d, err := mmapAlloc(size); err == nil {
			return d, true
		}
	}
	return reedsolomon.AllocAligned(1, size)[0], false
}

// allocMany appends up to n buffers of size bytes to dst. Caller must hold
// Pool.mu. Fast path: one contiguous region for best cache/TLB locality
// (critical for RS FFT work buffers). Slow path: gathers scattered buffers
// from individual free regions when no single region is large enough.
func (s *slab) allocMany(dst [][]byte, n, size int) [][]byte {
	if n == 0 {
		return dst
	}
	if flat, ok := s.alloc(n * size); ok {
		for i := range n {
			// full-slice expression keeps sub-buffers from appending into neighbors.
			dst = append(dst, flat[i*size:(i+1)*size:(i+1)*size])
		}
		return dst
	}
	for range n {
		buf, ok := s.alloc(size)
		if !ok {
			break
		}
		dst = append(dst, buf)
	}
	return dst
}

func (s *slab) alloc(size int) ([]byte, bool) {
	for i, r := range s.regions {
		if r.size < size {
			continue
		}
		// full-slice expression caps the buffer so a caller can't append
		// past its length into adjacent regions.
		buf := s.data[r.off : r.off+size : r.off+size]
		if r.size == size {
			s.regions = slices.Delete(s.regions, i, i+1)
		} else {
			s.regions[i] = region{r.off + size, r.size - size}
		}
		s.idleShrinks = 0
		return buf, true
	}
	return nil, false
}

// stats returns total free bytes and the size of the largest contiguous free
// region. Caller must hold Pool.mu. Used by [Pool.Stats].
func (s *slab) stats() (free, maxRegion int) {
	for _, r := range s.regions {
		free += r.size
		if r.size > maxRegion {
			maxRegion = r.size
		}
	}
	return free, maxRegion
}

func (s *slab) contains(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}
	p := uintptr(unsafe.Pointer(unsafe.SliceData(buf)))
	base := uintptr(unsafe.Pointer(unsafe.SliceData(s.data)))
	return p >= base && p < base+uintptr(s.size)
}

// freeBatch coalesces a sorted-by-offset run of same-slab buffers: adjacent
// ones merge into one region insertion, and a monotonic hint through
// coalesceAt keeps each insertion amortized O(1).
func (s *slab) freeBatch(bufs [][]byte) {
	var hint int
	base := uintptr(unsafe.Pointer(unsafe.SliceData(s.data)))
	var run region // zero size means "no pending run"

	flush := func() {
		if run.size == 0 {
			return
		}
		hint = s.coalesceAt(run, hint)
		run = region{}
	}

	for _, buf := range bufs {
		if len(buf) == 0 {
			continue
		}
		off := int(uintptr(unsafe.Pointer(unsafe.SliceData(buf))) - base)
		if off < 0 || len(buf) > s.size-off {
			continue // defense against overshoot / misaligned sub-slice
		}
		if run.off+run.size == off && run.size > 0 {
			run.size += len(buf)
			continue
		}
		flush()
		run = region{off, len(buf)}
	}
	flush()
}

func (s *slab) isFree() bool {
	return len(s.regions) == 1 && s.regions[0].off == 0 && s.regions[0].size == s.size
}

// coalesceAt inserts r into the sorted regions list, merging with adjacent
// regions. Scan starts at hint; returns r's final index so callers doing a
// monotonic sequence of inserts can resume from there for amortized O(1).
func (s *slab) coalesceAt(r region, hint int) int {
	i := min(hint, len(s.regions))
	for i < len(s.regions) && s.regions[i].off < r.off {
		i++
	}
	s.regions = slices.Insert(s.regions, i, r)

	if i+1 < len(s.regions) && s.regions[i].off+s.regions[i].size == s.regions[i+1].off {
		s.regions[i].size += s.regions[i+1].size
		s.regions = slices.Delete(s.regions, i+1, i+2)
	}
	if i > 0 && s.regions[i-1].off+s.regions[i-1].size == s.regions[i].off {
		s.regions[i-1].size += s.regions[i].size
		s.regions = slices.Delete(s.regions, i, i+1)
		i--
	}
	return i
}
