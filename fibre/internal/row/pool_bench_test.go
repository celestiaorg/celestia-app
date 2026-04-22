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
// when an exact-match bucket is primed: every iteration pops a free
// batch and puts it back, exercising the common case after startup.
func BenchmarkPool_GetPut_Reuse(b *testing.B) {
	p := New(benchMaxRow, benchRowCount)
	p.Put(p.Get(benchRowCount, 64)) // seed

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Put(p.Get(benchRowCount, 64))
	}
}

// BenchmarkPool_GetPut_Fallback measures the upward-slack fallback path.
// The 64-byte bucket is never populated; the 128-byte bucket holds the
// reusable batch, so every Get scans slots within the slack window
// before popping.
func BenchmarkPool_GetPut_Fallback(b *testing.B) {
	p := New(benchMaxRow, benchRowCount)
	p.Put(p.Get(benchRowCount, 128)) // seed the fallback-target bucket

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		// Each Get(64) falls back into slot for 128, pops its batch,
		// and Put returns it to the same 128 bucket (via batch.bucket).
		p.Put(p.Get(benchRowCount, 64))
	}
}

// BenchmarkPool_Concurrent stresses Pool.mu under contention.
func BenchmarkPool_Concurrent(b *testing.B) {
	p := New(benchMaxRow, benchRowCount)
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
// allocation under Pool.mu. Holds for the reuse case even though the
// reuse path itself is O(1) — any in-flight allocation (especially mmap)
// stalls everyone waiting on the lock.
//
// Shape: rowCount=1024 × rowSize=1024 → ~1 MiB batch, above mmapThreshold,
// so fresh allocs go through the mmap syscall. Worker 0 periodically
// asks for a novel size (no bucket → alloc); other workers hammer the
// reuse path.
//
// Reports p50/p99/p99.99/max as extra benchmark metrics; a bimodal
// distribution (p50 ≪ p99) is the fingerprint of lock-held alloc tail.
func BenchmarkPool_AllocContentionTail(b *testing.B) {
	const rowCount = 1024
	const baseSize = 1024    // batch size ≈ 1 MiB → mmap path
	const maxRow = 64 * 1024 // leaves room for novel sizes
	const allocEveryN = 256  // ~0.4% of ops force a fresh alloc
	const workers = 16

	p := New(maxRow, rowCount)
	p.Put(p.Get(rowCount, baseSize)) // seed the reuse bucket

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
					// novel size → exact bucket empty, outside slack → fresh alloc+mmap
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
