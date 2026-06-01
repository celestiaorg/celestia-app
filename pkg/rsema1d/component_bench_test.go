package rsema1d

import (
	"math/rand/v2"
	"runtime"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/klauspost/reedsolomon"
)

// BenchmarkLeopardRSExtend measures the Reed-Solomon extension alone (no
// hashing, no commitment), using the same encoder configuration the Coder
// uses. Reports MB/s over original data (K * rowSize) so it's directly
// comparable to BenchmarkCommitOnly's throughput numbers — the gap between the
// two is what tells us whether hashing or RS is the bottleneck.
func BenchmarkLeopardRSExtend(b *testing.B) {
	sizes := []struct {
		name    string
		k, n    int
		rowSize int
		workers int
	}{
		{"4096x12288x8192", 4096, 12288, 8192, runtime.NumCPU()},
		{"1024x1024x131072_w=auto", 1024, 1024, 131072, runtime.NumCPU()},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			workAlloc := newRetainAllocator(leopardEncodeWorkBuffers(sz.n), field.LeopardChunkSize)
			enc, err := reedsolomon.New(sz.k, sz.n,
				reedsolomon.WithLeopardGF16(true),
				reedsolomon.WithWorkAllocator(workAlloc))
			if err != nil {
				b.Fatal(err)
			}

			rng := rand.New(rand.NewPCG(1, 2))
			rows := make([][]byte, sz.k+sz.n)
			for i := range rows {
				rows[i] = make([]byte, sz.rowSize)
				if i < sz.k {
					for j := range rows[i] {
						rows[i][j] = byte(rng.Uint64())
					}
				}
			}

			b.SetBytes(int64(sz.k * sz.rowSize))
			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				// Clear parity rows in place; klauspost/reedsolomon's
				// Leopard16 path is in-place over parity slots.
				for i := sz.k; i < sz.k+sz.n; i++ {
					clear(rows[i])
				}
				if err := enc.Encode(rows); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCommitOnly isolates the commit step (BLAKE3-Bao row tree + RLC
// derive/compute + RLC Merkle tree + commitment) from RS extension. Inputs are
// pre-extended rows; the bench loop only invokes commit().
func BenchmarkCommitOnly(b *testing.B) {
	sizes := []struct {
		name    string
		k, n    int
		rowSize int
	}{
		{"4096x12288x8192", 4096, 12288, 8192},
		{"1024x1024x131072", 1024, 1024, 131072},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			coder, err := NewCoder(&Config{K: sz.k, N: sz.n, WorkerCount: runtime.NumCPU()})
			if err != nil {
				b.Fatal(err)
			}

			rng := rand.New(rand.NewPCG(3, 4))
			data := make([][]byte, sz.k)
			for i := range data {
				data[i] = make([]byte, sz.rowSize)
				for j := range data[i] {
					data[i][j] = byte(rng.Uint64())
				}
			}
			// One real Encode so we have valid extended rows to commit over.
			ed, err := coder.Encode(append(data, makeZeroRows(sz.n, sz.rowSize)...))
			if err != nil {
				b.Fatal(err)
			}
			extended := ed.rows

			b.SetBytes(int64(sz.k * sz.rowSize))
			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := coder.commit(extended); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func makeZeroRows(n, rowSize int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = make([]byte, rowSize)
	}
	return out
}
