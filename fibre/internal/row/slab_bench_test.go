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
// a codec-sized slab: reusing a warm slab from the pool costs nothing here.
// Compare with BenchmarkMmapTouch, which pays a page fault and kernel zeroing
// per page on every iteration — the cost the pool removes.
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

// BenchmarkMmapTouch is BenchmarkSlabTouch without a pool: mmap, first write to
// every page, munmap.
func BenchmarkMmapTouch(b *testing.B) {
	for _, s := range slabShapes {
		for _, workers := range []int{1, 16} {
			b.Run(s.name+"/workers="+strconv.Itoa(workers), func(b *testing.B) {
				b.SetBytes(int64(s.rows * s.size))
				parallelN(b.N, workers, func() {
					buf, err := mmapAlloc(s.rows * s.size)
					if err != nil {
						b.Error(err)
						return
					}
					for i := 0; i < len(buf); i += 4096 {
						buf[i] = 1
					}
					munmap(buf)
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
