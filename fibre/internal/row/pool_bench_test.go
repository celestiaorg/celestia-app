package row

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	benchRowCount = 6
	benchMaxRow   = 4096
)

// BenchmarkPool_GetPut_Reuse measures the hot-path steady-state cost
// when a slab of the exact size is free: every iteration reuses it and
// puts it back, exercising the common case after startup.
func BenchmarkPool_GetPut_Reuse(b *testing.B) {
	p := NewPool(benchMaxRow, benchRowCount)
	p.Put(p.Get(benchRowCount, 64)) // seed

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Put(p.Get(benchRowCount, 64))
	}
}

// BenchmarkPool_Concurrent stresses lock contention on the free list.
func BenchmarkPool_Concurrent(b *testing.B) {
	p := NewPool(benchMaxRow, benchRowCount)
	p.Put(p.Get(benchRowCount, 64)) // seed

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.Put(p.Get(benchRowCount, 64))
		}
	})
}

// BenchmarkPool_AllocContentionTail measures the tail-latency penalty
// reuse-path Gets pay while another goroutine is doing a fresh
// allocation. Worker 0 periodically asks for a novel size (no free slab
// fits → alloc+mmap); the others hammer the reuse path. Reports
// p50/p99/p99.99/max — a bimodal distribution (p50 ≪ p99) signals
// lock-held alloc tail.
//
// Shape: ~1 MiB slabs (above mmapThreshold) so fresh allocs go
// through mmap.
func BenchmarkPool_AllocContentionTail(b *testing.B) {
	const rowCount = 1024
	const baseSize = 1024    // slab size ≈ 1 MiB → mmap path
	const maxRow = 64 * 1024 // leaves room for novel sizes
	const allocEveryN = 256  // ~0.4% of ops force a fresh alloc
	const workers = 16

	p := NewPool(maxRow, rowCount)
	p.Put(p.Get(rowCount, baseSize)) // seed the free list

	workerLat := make([][]time.Duration, workers)
	for i := range workerLat {
		workerLat[i] = make([]time.Duration, 0, b.N/workers+16)
	}
	var counter atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)

	b.ResetTimer()
	for w := range workers {
		go func(id int) {
			defer wg.Done()
			for {
				i := counter.Add(1) - 1
				if i >= int64(b.N) {
					return
				}
				size := baseSize
				if id == 0 && i > 0 && i%allocEveryN == 0 {
					// novel size → no free slab fits → fresh alloc+mmap
					size = baseSize + rowSizeAlign*int(1+(i/allocEveryN)%32)
					if size > maxRow {
						size = baseSize
					}
				}
				t0 := time.Now()
				bufs := p.Get(rowCount, size)
				dt := time.Since(t0)
				p.Put(bufs)
				workerLat[id] = append(workerLat[id], dt)
			}
		}(w)
	}
	wg.Wait()
	b.StopTimer()

	total := 0
	for _, wl := range workerLat {
		total += len(wl)
	}
	all := make([]time.Duration, 0, total)
	for _, wl := range workerLat {
		all = append(all, wl...)
	}
	slices.Sort(all)

	if len(all) == 0 {
		return
	}
	b.ReportMetric(float64(all[len(all)*50/100].Nanoseconds()), "p50-ns")
	b.ReportMetric(float64(all[len(all)*99/100].Nanoseconds()), "p99-ns")
	b.ReportMetric(float64(all[min(len(all)-1, len(all)*9999/10000)].Nanoseconds()), "p99.99-ns")
	b.ReportMetric(float64(all[len(all)-1].Nanoseconds()), "max-ns")
}
