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
