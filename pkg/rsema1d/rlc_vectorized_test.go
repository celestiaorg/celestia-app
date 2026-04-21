package rsema1d

import (
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

// TestComputeRLCVectorizedMatchesScalar verifies the vectorized SIMD kernel
// produces the same []GF128 as the per-row scalar loop across a range of
// eligible K/rowSize combinations and both worker counts.
func TestComputeRLCVectorizedMatchesScalar(t *testing.T) {
	cases := []struct{ k, rowSize int }{
		{32, 64},
		{32, 128},
		{64, 64},
		{256, 256},
		{1024, 1024},
		{1024, 8192},
		{4096, 4096},
		// K values that are not a multiple of symbolsPerChunk to exercise
		// the internal zero-row padding path.
		{1, 64},
		{17, 128},
		{33, 64},
		{100, 256},
		{1023, 1024},
	}
	for _, tc := range cases {
		rows := make([][]byte, tc.k)
		r := rand.New(rand.NewPCG(uint64(tc.k), uint64(tc.rowSize)))
		for i := range rows {
			rows[i] = make([]byte, tc.rowSize)
			for j := range rows[i] {
				rows[i][j] = byte(r.IntN(256))
			}
		}
		var rowRoot [32]byte
		for i := range rowRoot {
			rowRoot[i] = byte(r.IntN(256))
		}
		coeffs := deriveCoefficients(rowRoot, tc.rowSize)
		cfg := &Config{K: tc.k, N: tc.k, RowSize: tc.rowSize, WorkerCount: 1}

		want := computeRLCOrig(rows, coeffs, cfg)
		for _, workers := range []int{1, 4} {
			cfg.WorkerCount = workers
			got := computeRLCVectorized(rows, coeffs, cfg)
			if len(want) != len(got) {
				t.Fatalf("k=%d rs=%d workers=%d length mismatch", tc.k, tc.rowSize, workers)
			}
			for i := range want {
				if !field.Equal128(want[i], got[i]) {
					t.Fatalf("k=%d rs=%d workers=%d row %d mismatch: want %v got %v",
						tc.k, tc.rowSize, workers, i, want[i], got[i])
				}
			}
		}
	}
}

// BenchmarkComputeRLCVectorized measures the vectorized SIMD RLC kernel at
// the largest single-row size in the matrix — 128MB total, K=1024 → 128KB
// per row. Both single-worker and default-worker variants are covered.
func BenchmarkComputeRLCVectorized(b *testing.B) {
	configs := []struct {
		name        string
		bytes, k, n int
		workers     int
	}{
		{"size=128MB/k=1024/n=1024", 128 << 20, 1024, 1024, 1},
		{"size=128MB/k=1024/n=1024/workers=16", 128 << 20, 1024, 1024, 16},
		{"size=128MB/k=1024/n=3072", 128 << 20, 1024, 3072, 1},
		{"size=128MB/k=1024/n=3072/workers=16", 128 << 20, 1024, 3072, 16},
	}
	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			rowSize := cfg.bytes / cfg.k
			codecConfig := &Config{
				K: cfg.k, N: cfg.n, RowSize: rowSize, WorkerCount: cfg.workers,
			}
			rowRoot := [32]byte{1, 2, 3, 4}
			coeffs := deriveCoefficients(rowRoot, rowSize)

			data := make([][]byte, cfg.k)
			r := rand.New(rand.NewPCG(uint64(cfg.k), uint64(rowSize)))
			for i := range data {
				data[i] = make([]byte, rowSize)
				for j := range data[i] {
					data[i][j] = byte(r.IntN(256))
				}
			}

			b.SetBytes(int64(cfg.bytes))
			b.ResetTimer()
			for range b.N {
				_ = computeRLCVectorized(data, coeffs, codecConfig)
			}
		})
	}
}
