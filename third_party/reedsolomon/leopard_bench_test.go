package reedsolomon

import (
	"math/rand"
	"slices"
	"sync"
	"testing"
)

// fixedWorkAllocator hands out one reusable buffer set; for single-goroutine
// encoders it removes pool churn from benchmark timings.
type fixedWorkAllocator struct{ bufs [][]byte }

func (a *fixedWorkAllocator) Get(n, size int) [][]byte {
	if len(a.bufs) < n || cap(a.bufs[0]) < size {
		a.bufs = AllocAligned(n, size)
	}
	w := a.bufs[:n]
	for i := range w {
		w[i] = w[i][:size]
	}
	return w
}

func (a *fixedWorkAllocator) Put([][]byte) {}

const benchWorkers = 16

// benchSets holds one encoder and shard set per worker for the production
// shape. They are allocated once per process: a fresh 8 GiB per b.Run pass
// would add page-fault time to the measurement and exhaust memory.
var benchSets struct {
	once sync.Once
	encs [benchWorkers]Encoder
	sets [benchWorkers][][]byte
}

func benchProductionSets(b *testing.B) {
	b.Helper()
	const data, parity, size = 4096, 12288, 32 << 10
	benchSets.once.Do(func() {
		for w := range benchWorkers {
			enc, err := New(data, parity, WithLeopardGF16(true), WithWorkAllocator(&fixedWorkAllocator{}))
			if err != nil {
				b.Fatal(err)
			}
			shards := AllocAligned(data+parity, size)
			rng := rand.New(rand.NewSource(int64(w + 1)))
			for i := range data {
				rng.Read(shards[i])
			}
			// Touch parity so page faults happen here, not in the timed loop.
			if err := enc.Encode(shards); err != nil {
				b.Fatal(err)
			}
			benchSets.encs[w], benchSets.sets[w] = enc, shards
		}
	})
	b.SetBytes(int64(data * size))
}

// BenchmarkLeopardGF16Production measures the fibre encode shape
// (4096 data + 12288 parity, 32 KiB shards) alone and with 16 encoders
// running concurrently, each on its own goroutine, encoder and shards.
func BenchmarkLeopardGF16Production(b *testing.B) {
	b.Run("single", func(b *testing.B) {
		benchProductionSets(b)
		enc, shards := benchSets.encs[0], benchSets.sets[0]
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := enc.Encode(shards); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("conc16", func(b *testing.B) {
		benchProductionSets(b)
		b.SetBytes(int64(benchWorkers) * int64(4096*(32<<10)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup
			var errs [benchWorkers]error
			for w := range benchWorkers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs[w] = benchSets.encs[w].Encode(benchSets.sets[w])
				}()
			}
			wg.Wait()
			for _, err := range errs {
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// benchReconSets holds one default-allocator decoder and erased shard view
// per worker and erasure pattern, built on the benchSets shards.
var benchReconSets struct {
	once sync.Once
	encs [benchWorkers]Encoder
	// in[pattern][worker] has the missing slots as len 0, cap shardSize.
	in [2][benchWorkers][][]byte
}

func benchReconstructSets(b *testing.B) {
	b.Helper()
	benchProductionSets(b)
	const data, parity, size = 4096, 12288, 32 << 10
	benchReconSets.once.Do(func() {
		for w := range benchWorkers {
			enc, err := New(data, parity, WithLeopardGF16(true))
			if err != nil {
				b.Fatal(err)
			}
			benchReconSets.encs[w] = enc
			shards := benchSets.sets[w]
			// Worst case: exactly data shards, all parity.
			keep := make([]bool, data+parity)
			for i := data; i < 2*data; i++ {
				keep[i] = true
			}
			benchReconSets.in[0][w] = benchErase(shards, keep)
			// Random: exactly data shards, uniformly random.
			rng := rand.New(rand.NewSource(int64(100 + w)))
			clear(keep)
			for _, i := range rng.Perm(data + parity)[:data] {
				keep[i] = true
			}
			benchReconSets.in[1][w] = benchErase(shards, keep)
		}
	})
}

// benchErase returns a shard view whose missing slots reuse the shard's own
// storage with length 0, so ReconstructData writes back in place. Callers
// clone the view per call since ReconstructData extends the missing slots.
func benchErase(shards [][]byte, keep []bool) [][]byte {
	in := make([][]byte, len(shards))
	for i, s := range shards {
		if keep[i] {
			in[i] = s
		} else {
			in[i] = s[:0]
		}
	}
	return in
}

// BenchmarkLeopardGF16Reconstruct measures ReconstructData at the fibre shape
// for the all-parity and random-K erasures, alone and with 16 decoders
// running concurrently.
func BenchmarkLeopardGF16Reconstruct(b *testing.B) {
	for pi, pat := range []string{"parity", "random"} {
		b.Run(pat+"/single", func(b *testing.B) {
			benchReconstructSets(b)
			enc, in := benchReconSets.encs[0], benchReconSets.in[pi][0]
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := enc.ReconstructData(slices.Clone(in)); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(pat+"/conc16", func(b *testing.B) {
			benchReconstructSets(b)
			b.SetBytes(int64(benchWorkers) * int64(4096*(32<<10)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var wg sync.WaitGroup
				var errs [benchWorkers]error
				for w := range benchWorkers {
					wg.Add(1)
					go func() {
						defer wg.Done()
						errs[w] = benchReconSets.encs[w].ReconstructData(slices.Clone(benchReconSets.in[pi][w]))
					}()
				}
				wg.Wait()
				for _, err := range errs {
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
