//go:build arm64 && !noasm && !nopshufb && !race

package rlc

import (
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"
)

// TestPolyBasisMatchesField checks that the polynomial-basis product equals
// Leopard's GF(2^16) multiply.
func TestPolyBasisMatchesField(t *testing.T) {
	polyOnce.Do(initPoly)
	clmul := func(a, b uint16) uint32 {
		var x uint32
		for i := range 16 {
			if b>>i&1 == 1 {
				x ^= uint32(a) << i
			}
		}
		return x
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range 1 << 16 {
		v := uint16(i)
		if fromPoly(toPoly(v)) != v {
			t.Fatalf("basis round trip fails for %#x", v)
		}
		b := uint16(rng.Uint32())
		want := field.Mul128(v, field.GF128{b})[0]
		if got := fromPoly(reducePoly(clmul(toPoly(v), toPoly(b)))); got != want {
			t.Fatalf("%#x*%#x: got %#x want %#x", v, b, got, want)
		}
	}
}

// TestComputeFastMatchesTranspose checks the PMULL RLC against the
// transpose path and the scalar reference, including odd K and short rows.
func TestComputeFastMatchesTranspose(t *testing.T) {
	cases := []struct{ k, rowSize int }{{1, 64}, {3, 128}, {17, 2112}, {32, 64}, {100, 4096}, {1024, 32768}}
	for _, tc := range cases {
		r := rand.New(rand.NewPCG(uint64(tc.k), uint64(tc.rowSize)))
		rows := make([][]byte, tc.k)
		for i := range rows {
			// unaligned rows
			rows[i] = make([]byte, tc.rowSize+1)[1:]
			for j := range rows[i] {
				rows[i][j] = byte(r.Uint32())
			}
		}
		coeffs := DeriveCoefficients([32]byte{byte(tc.k)}, tc.k, tc.k, tc.rowSize, 1)
		usePoly = false
		want := Compute(rows, coeffs, 1)
		usePoly = true
		for _, workers := range []int{1, 4, 16} {
			got, ok := computeFast(rows, coeffs, workers)
			if !ok {
				t.Fatal("PMULL path unavailable")
			}
			for i := range want {
				if !field.Equal128(want[i], got[i]) || !field.Equal128(ComputeRow(rows[i], coeffs), got[i]) {
					t.Fatalf("k=%d rowSize=%d workers=%d row %d differs", tc.k, tc.rowSize, workers, i)
				}
			}
		}
	}
}
