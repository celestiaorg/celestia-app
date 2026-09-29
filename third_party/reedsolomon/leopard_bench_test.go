package reedsolomon

import (
	"math/rand"
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

func benchProductionSet(b *testing.B, data, parity, size int, seed int64) (Encoder, [][]byte) {
	b.Helper()
	enc, err := New(data, parity, WithLeopardGF16(true), WithWorkAllocator(&fixedWorkAllocator{}))
	if err != nil {
		b.Fatal(err)
	}
	shards := AllocAligned(data+parity, size)
	rng := rand.New(rand.NewSource(seed))
	for i := range data {
		rng.Read(shards[i])
	}
	return enc, shards
}

// BenchmarkLeopardGF16Production measures the fibre encode shape
// (4096 data + 12288 parity, 32 KiB shards) alone and with 16 encoders
// running concurrently, each on its own goroutine, encoder and shards.
func BenchmarkLeopardGF16Production(b *testing.B) {
	const data, parity, size = 4096, 12288, 32 << 10
	b.Run("single", func(b *testing.B) {
		enc, shards := benchProductionSet(b, data, parity, size, 1)
		b.SetBytes(int64(data * size))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := enc.Encode(shards); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("conc16", func(b *testing.B) {
		const workers = 16
		encs := make([]Encoder, workers)
		sets := make([][][]byte, workers)
		for w := range workers {
			encs[w], sets[w] = benchProductionSet(b, data, parity, size, int64(w+1))
		}
		b.SetBytes(int64(workers * data * size))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup
			errs := make([]error, workers)
			for w := range workers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs[w] = encs[w].Encode(sets[w])
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
