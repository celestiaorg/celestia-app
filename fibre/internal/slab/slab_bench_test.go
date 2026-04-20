package slab

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// BenchmarkAllocContiguous measures the fast path: clean slab, contiguous allocation.
func BenchmarkAllocContiguous(b *testing.B) {
	cases := []struct {
		name string
		n    int
		size int
	}{
		{"work/max_blob", 32768, 32832},
		{"work/1MiB_blob", 32768, 320},
		{"parity/max_blob", 12290, 32832},
		{"parity/1MiB_blob", 12290, 320},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			p := New()
			// warm up: first Get creates the slab.
			w := p.Get(tc.n, tc.size)
			p.Put(w)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				bufs := p.Get(tc.n, tc.size)
				p.Put(bufs)
			}
		})
	}
}

// BenchmarkAllocFragmented measures the slow path: slab fragmented by partial
// frees, forcing scattered allocation.
func BenchmarkAllocFragmented(b *testing.B) {
	cases := []struct {
		name string
		n    int
		size int
	}{
		{"parity/max_blob", 12290, 32832},
		{"parity/1MiB_blob", 12290, 320},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			p := New()

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				// create fragmentation: allocate, free every other buffer.
				bufs := p.Get(tc.n, tc.size)
				odd := make([][]byte, 0, tc.n/2)
				even := make([][]byte, 0, tc.n/2)
				for i, buf := range bufs {
					if i%2 == 0 {
						even = append(even, buf)
					} else {
						odd = append(odd, buf)
					}
				}
				p.Put(odd) // free half, creating fragmentation
				b.StartTimer()

				// allocate into the fragmented slab — measures scattered path.
				more := p.Get(tc.n/2, tc.size)
				p.Put(more)

				b.StopTimer()
				p.Put(even) // clean up
				b.StartTimer()
			}
		})
	}
}

// BenchmarkReuseAfterPartial models the production release→reuse pattern:
// blob A allocates a group, one validator shard is released (shuffled
// offsets, matching Set.Assign), then a follow-up Get of varying size
// measures whether the freed holes get reused or a new slab grows.
// Sizes model a small-medium blob to keep memory reasonable.
func BenchmarkReuseAfterPartial(b *testing.B) {
	const (
		parityN = 514 // N+2 for a small-medium blob
		rowSize = 512
		shardN  = 64 // ~one validator's worth out of ~8
	)
	cases := []struct {
		name  string
		nextN int
	}{
		{"small", 32},      // fits entirely inside the freed holes
		{"same-size", 514}, // forces a grow; measures grow + re-allocation
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var totalSlabs, iters int

			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				p := New()
				bufsA := p.Get(parityN, rowSize)
				rng := rand.New(rand.NewPCG(42, 42))
				perm := rng.Perm(parityN)[:shardN]
				shard := make([][]byte, shardN)
				for i, idx := range perm {
					shard[i] = bufsA[idx]
				}
				p.Put(shard)
				b.StartTimer()

				_ = p.Get(tc.nextN, rowSize)

				totalSlabs += p.Slabs()
				iters++
				_ = bufsA
			}
			if iters > 0 {
				b.ReportMetric(float64(totalSlabs)/float64(iters), "slabs/op")
			}
		})
	}
}

// BenchmarkAdaptiveGrow measures the grow path under simulated concurrent
// demand: Gets stack up before any Puts, forcing successive slab grows sized
// only to each request's remaining deficit.
func BenchmarkAdaptiveGrow(b *testing.B) {
	cases := []struct {
		name       string
		concurrent int // outstanding Gets at peak
		n          int
		size       int
	}{
		{"2x/parity", 2, 12290, 32832},
		{"4x/parity", 4, 12290, 32832},
		{"2x/small", 2, 4096, 320},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				p := New()
				outstanding := make([][][]byte, 0, tc.concurrent)
				for range tc.concurrent {
					outstanding = append(outstanding, p.Get(tc.n, tc.size))
				}
				for _, bufs := range outstanding {
					p.Put(bufs)
				}
			}
		})
	}
}

// BenchmarkPutPartial measures per-validator release. The "contiguous"
// variant frees each validator's aligned chunk (cheap — coalesce merges
// left). The "shuffled" variant mirrors Set.Assign's ChaCha8 shuffle with
// validators releasing uniformly-random row subsets — the production
// release pattern. Two validator counts cover the cases that matter: v=1
// (one big Put) and v=10 (many small shuffled Puts). v=5 runs within 5%
// of v=10 on all measured workloads.
func BenchmarkPutPartial(b *testing.B) {
	const n, size = 12290, 32832
	validators := []int{1, 10}

	for _, nval := range validators {
		b.Run(fmt.Sprintf("contiguous/v=%d", nval), func(b *testing.B) {
			p := New()
			rowsPerVal := n / nval
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				bufs := p.Get(n, size)
				for v := range nval {
					start, end := v*rowsPerVal, (v+1)*rowsPerVal
					if v == nval-1 {
						end = n
					}
					p.Put(bufs[start:end])
				}
			}
		})

		b.Run(fmt.Sprintf("shuffled/v=%d", nval), func(b *testing.B) {
			p := New()
			// precompute shuffled shards so the cost shows up in the measured
			// Put path, not the shuffle setup.
			rng := rand.New(rand.NewPCG(42, 42))
			perm := rng.Perm(n)
			per := n / nval
			shards := make([][]int, nval)
			for v := range nval {
				start, end := v*per, (v+1)*per
				if v == nval-1 {
					end = n
				}
				shards[v] = perm[start:end]
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				bufs := p.Get(n, size)
				shard := make([][]byte, 0, per+1)
				for _, idxs := range shards {
					shard = shard[:0]
					for _, i := range idxs {
						shard = append(shard, bufs[i])
					}
					p.Put(shard)
				}
			}
		})
	}
}
