//go:build arm64 && !noasm && !nopshufb && !race

package rlc

import (
	"bytes"
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"
	"golang.org/x/sys/cpu"
)

// requirePMULL skips when the CPU lacks the instructions the kernels use, since
// the assembly would SIGILL rather than fall back.
func requirePMULL(t *testing.T) {
	t.Helper()
	if !cpu.ARM64.HasASIMD || !cpu.ARM64.HasPMULL || !cpu.ARM64.HasSHA3 {
		t.Skip("PMULL/EOR3 unavailable on this CPU")
	}
}

// TestKernelsMatchGoTwin checks the PMULL/EOR3 assembly produces byte-identical
// output to the pure-Go twins polyConvertGo and polyAccumGo.
func TestKernelsMatchGoTwin(t *testing.T) {
	requirePMULL(t)
	polyOnce.Do(initPoly)
	rng := rand.New(rand.NewPCG(42, 99))
	for _, rowSize := range []int{64, 128, 320, 4096, 32768} {
		blocks := rowSize / (2 * polyBlockSymbols)

		src := make([]byte, rowSize)
		for i := range src {
			src[i] = byte(rng.Uint32())
		}
		dstASM := make([]byte, blocks*polyOperandBytes)
		dstGo := make([]byte, blocks*polyOperandBytes)
		polyConvertNEON(dstASM, src, &toPolyNibbles)
		polyConvertGo(dstGo, src, &toPolyNibbles)
		if !bytes.Equal(dstASM, dstGo) {
			t.Fatalf("rowSize=%d: polyConvertNEON differs from polyConvertGo", rowSize)
		}

		// Accumulate the converted operands against random coefficient tables,
		// starting from identical non-zero accumulators to exercise the XOR
		// read-modify-write.
		b := make([]byte, blocks*polyCoeffBytes)
		for i := range b {
			b[i] = byte(rng.Uint32())
		}
		var accASM, accGo [12][16]byte
		for i := range accASM {
			for j := range accASM[i] {
				v := byte(rng.Uint32())
				accASM[i][j] = v
				accGo[i][j] = v
			}
		}
		polyAccum4NEON(&accASM, dstGo, b)
		polyAccumGo(&accGo, dstGo, b)
		if accASM != accGo {
			t.Fatalf("rowSize=%d: polyAccum4NEON differs from polyAccumGo", rowSize)
		}
	}
}

// TestComputeFastMatchesGo checks the dispatched PMULL path against the pure-Go
// twin and the scalar reference, including odd K, short and long rows, and
// unaligned buffers.
func TestComputeFastMatchesGo(t *testing.T) {
	requirePMULL(t)
	cases := []struct{ k, rowSize int }{{1, 64}, {3, 128}, {17, 2112}, {32, 64}, {100, 4096}, {1024, 32768}}
	for _, tc := range cases {
		rows, coeffs := randomRLCInputs(tc.k, tc.rowSize)
		want := computeGo(rows, coeffs, 1)
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
