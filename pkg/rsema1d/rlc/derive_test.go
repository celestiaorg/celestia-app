package rlc_test

import (
	"runtime"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
)

// TestDeriveDeterministic verifies Derive returns the same coefficients on
// repeated calls with the same inputs, and that the serial (workers=1) and
// parallel (workers>1) paths produce identical output across the
// minParallelSymbols rowSize threshold (512 symbols = 1024 bytes).
func TestDeriveDeterministic(t *testing.T) {
	rowRoot := [32]byte{1, 2, 3, 4, 5}
	cases := []struct{ k, n, rowSize int }{
		{1, 1, 2},          // 1 symbol — always serial
		{32, 32, 64},       // 32 symbols — below threshold
		{1024, 1024, 1022}, // 511 symbols — just under threshold
		{1024, 1024, 1024}, // 512 symbols — at threshold
		{1024, 1024, 8192}, // 4096 symbols — parallel
	}
	for _, tc := range cases {
		serial := rlc.Derive(rowRoot, tc.k, tc.n, tc.rowSize, 1)
		again := rlc.Derive(rowRoot, tc.k, tc.n, tc.rowSize, 1)
		parallel := rlc.Derive(rowRoot, tc.k, tc.n, tc.rowSize, 4)
		if got, want := len(serial), tc.rowSize/2; got != want {
			t.Errorf("k=%d n=%d rs=%d: len got %d want %d", tc.k, tc.n, tc.rowSize, got, want)
			continue
		}
		for i := range serial {
			if !field.Equal128(serial[i], again[i]) {
				t.Fatalf("k=%d n=%d rs=%d: nondeterministic at i=%d", tc.k, tc.n, tc.rowSize, i)
			}
			if !field.Equal128(serial[i], parallel[i]) {
				t.Fatalf("k=%d n=%d rs=%d: parallel/serial diverge at i=%d", tc.k, tc.n, tc.rowSize, i)
			}
		}
	}
}

// TestDeriveSensitivity verifies all four inputs (rowRoot, k, n, rowSize) are
// mixed into the Fiat-Shamir seed: changing any of them changes the
// coefficients.
func TestDeriveSensitivity(t *testing.T) {
	base := rlc.Derive([32]byte{1}, 100, 200, 256, 1)

	if equal128s(base, rlc.Derive([32]byte{2}, 100, 200, 256, 1)) {
		t.Error("changing rowRoot did not change coefficients")
	}
	if equal128s(base, rlc.Derive([32]byte{1}, 101, 200, 256, 1)) {
		t.Error("changing k did not change coefficients")
	}
	if equal128s(base, rlc.Derive([32]byte{1}, 100, 201, 256, 1)) {
		t.Error("changing n did not change coefficients")
	}
	// Changing rowSize also changes the slice length, so compare the leading
	// len(base) coefficients of a longer rowSize derivation — they must differ
	// from base if rowSize is properly bound into the seed.
	other := rlc.Derive([32]byte{1}, 100, 200, 258, 1)
	if len(other) <= len(base) {
		t.Fatalf("expected longer derivation: len=%d base=%d", len(other), len(base))
	}
	if equal128s(base, other[:len(base)]) {
		t.Error("changing rowSize did not change coefficients (prefix matches)")
	}
}

// TestDeriveEmptyRowSize verifies Derive(_, _, _, 0) returns an empty slice
// rather than panicking on the runtime.GOMAXPROCS / chunk-size math.
func TestDeriveEmptyRowSize(t *testing.T) {
	got := rlc.Derive([32]byte{}, 1, 1, 0, 1)
	if len(got) != 0 {
		t.Errorf("expected empty slice, got len %d", len(got))
	}
}

func equal128s(a, b rlc.Vector) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !field.Equal128(a[i], b[i]) {
			return false
		}
	}
	return true
}

// BenchmarkDerive measures Fiat-Shamir coefficient derivation across the
// serial/parallel rowSize threshold. workers=1 forces the serial path;
// workers=runtime.GOMAXPROCS exercises the parallel fan-out above
// minParallelSymbols (rowSize >= 1024 bytes / 512 symbols).
func BenchmarkDerive(b *testing.B) {
	rowRoot := [32]byte{1, 2, 3, 4}
	cases := []struct {
		name              string
		k, n, rowSize, ws int
	}{
		{"rs=64/serial", 1024, 1024, 64, 1},
		{"rs=1024/serial", 1024, 1024, 1024, 1},
		{"rs=8192/serial", 1024, 1024, 8192, 1},
		{"rs=8192/workers=GOMAXPROCS", 1024, 1024, 8192, 0},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			ws := tc.ws
			if ws == 0 {
				ws = runtime.GOMAXPROCS(0)
			}
			b.SetBytes(int64(tc.rowSize))
			b.ResetTimer()
			for range b.N {
				_ = rlc.Derive(rowRoot, tc.k, tc.n, tc.rowSize, ws)
			}
		})
	}
}
