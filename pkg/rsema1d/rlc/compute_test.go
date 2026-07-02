package rlc_test

import (
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/rlc"
)

// TestComputeMatchesScalar verifies the vectorized SIMD kernel produces the
// same []GF128 as the per-row scalar reference across a range of eligible
// K/rowSize combinations and both worker counts.
func TestComputeMatchesScalar(t *testing.T) {
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
		coeffs := rlc.DeriveCoefficients(rowRoot, tc.k, tc.k, tc.rowSize, 1)

		want := make(rlc.Vector, tc.k)
		for i, row := range rows {
			want[i] = rlc.ComputeRow(row, coeffs)
		}
		for _, workers := range []int{1, 4} {
			got := rlc.Compute(rows, coeffs, workers)
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

// TestComputeLinearity verifies the RLC operator is linear over GF(2):
// Compute(a) XOR Compute(b) == Compute(a XOR b), where XOR is component-wise
// over rows and byte-wise within each row. Linearity is the property that
// lets the protocol extend RLC values through Reed-Solomon and have them
// commute with per-row RLC computation.
func TestComputeLinearity(t *testing.T) {
	const k, rowSize = 32, 256
	r := rand.New(rand.NewPCG(7, 11))
	a := make([][]byte, k)
	b := make([][]byte, k)
	sum := make([][]byte, k)
	for i := range a {
		a[i] = make([]byte, rowSize)
		b[i] = make([]byte, rowSize)
		sum[i] = make([]byte, rowSize)
		for j := range a[i] {
			a[i][j] = byte(r.IntN(256))
			b[i][j] = byte(r.IntN(256))
			sum[i][j] = a[i][j] ^ b[i][j] // GF(2^16) addition is byte-wise XOR
		}
	}

	var rowRoot [32]byte
	for i := range rowRoot {
		rowRoot[i] = byte(r.IntN(256))
	}
	coeffs := rlc.DeriveCoefficients(rowRoot, k, k, rowSize, 1)

	rlcA := rlc.Compute(a, coeffs, 1)
	rlcB := rlc.Compute(b, coeffs, 1)
	rlcSum := rlc.Compute(sum, coeffs, 1)

	for i := range rlcSum {
		want := field.Add128(rlcA[i], rlcB[i])
		if !field.Equal128(rlcSum[i], want) {
			t.Fatalf("row %d: RLC(a XOR b) != RLC(a) XOR RLC(b): got %v want %v", i, rlcSum[i], want)
		}
	}
}

// BenchmarkCompute measures the vectorized SIMD RLC kernel at the largest
// single-row size in the matrix — 128MB total, K=1024 → 128KB per row. Both
// single-worker and 16-worker variants are covered for k=1024 with n=1024
// and n=3072.
func BenchmarkCompute(b *testing.B) {
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
			rowRoot := [32]byte{1, 2, 3, 4}
			coeffs := rlc.DeriveCoefficients(rowRoot, cfg.k, cfg.n, rowSize, cfg.workers)

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
				_ = rlc.Compute(data, coeffs, cfg.workers)
			}
		})
	}
}
