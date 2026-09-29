package rsema1d

import (
	"fmt"
	"sync"
	"testing"
)

// BenchmarkReconstructData measures RS data recovery alone at the fibre v0
// shape, for every worker count (which sets the column split) and for 16
// concurrent reconstructs sharing the workers.
func BenchmarkReconstructData(b *testing.B) {
	const k, n, rowSize = 4096, 12288, 32768
	c, err := NewCoder(&Config{K: k, N: n, WorkerCount: 16})
	if err != nil {
		b.Fatal(err)
	}
	full := encodedRows(b, c, rowSize, 1)
	patterns := presentPatterns(k, n, 1)
	for _, name := range []string{"all_parity", "random"} {
		present := patterns[name]
		for _, workers := range []int{1, 2, 4, 8, 16} {
			b.Run(fmt.Sprintf("%s/workers=%d", name, workers), func(b *testing.B) {
				c.config.WorkerCount = workers
				rows := selectRows(full, present, k, rowSize)
				b.SetBytes(k * rowSize)
				for b.Loop() {
					b.StopTimer()
					for i := range k {
						if !present[i] {
							rows[i] = rows[i][:0]
						}
					}
					b.StartTimer()
					if err := c.reconstructData(rows); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
		b.Run(fmt.Sprintf("%s/concurrent=16", name), func(b *testing.B) {
			c.config.WorkerCount = 16
			const jobs = 16
			all := make([][][]byte, jobs)
			for j := range all {
				all[j] = selectRows(full, present, k, rowSize)
			}
			b.SetBytes(jobs * k * rowSize)
			for b.Loop() {
				var wg sync.WaitGroup
				for j := range jobs {
					wg.Add(1)
					go func() {
						defer wg.Done()
						rows := all[j]
						for i := range k {
							if !present[i] {
								rows[i] = rows[i][:0]
							}
						}
						if err := c.reconstructData(rows); err != nil {
							b.Error(err)
						}
					}()
				}
				wg.Wait()
			}
		})
	}
}
