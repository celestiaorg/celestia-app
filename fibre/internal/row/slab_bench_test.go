package row

import (
	"strconv"
	"sync"
	"testing"
)

// Codec slab shapes for a 128 MiB blob: parity+partial rows, reconstruction
// work rows, and encode work rows (one part).
var slabShapes = []struct {
	name       string
	rows, size int
}{
	{"parity-384MiB", 12290, 32768},
	{"work-514MiB", 32768, 16448},
	{"work-1GiB", 32768, 32768},
}

// BenchmarkSlabTouch measures a Get, a first write to every page, and a Put of
// a codec-sized slab: reusing a warm slab from the pool saves the page fault
// and kernel zeroing per page that a fresh mapping pays.
func BenchmarkSlabTouch(b *testing.B) {
	for _, s := range slabShapes {
		for _, workers := range []int{1, 16} {
			b.Run(s.name+"/workers="+strconv.Itoa(workers), func(b *testing.B) {
				p := NewPool(s.size, s.rows)
				b.SetBytes(int64(s.rows * s.size))
				b.ResetTimer()
				parallelN(b.N, workers, func() {
					rows := p.Get(s.rows, s.size)
					for _, r := range rows {
						for i := 0; i < len(r); i += 4096 {
							r[i] = 1
						}
					}
					p.Put(rows)
				})
			})
		}
	}
}

func parallelN(n, workers int, f func()) {
	var wg sync.WaitGroup
	next := make(chan struct{})
	for range workers {
		wg.Go(func() {
			for range next {
				f()
			}
		})
	}
	for range n {
		next <- struct{}{}
	}
	close(next)
	wg.Wait()
}
