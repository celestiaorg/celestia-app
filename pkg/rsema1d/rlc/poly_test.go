package rlc

import (
	"math/rand/v2"
	"os"
	"runtime"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"
)

// TestPolyBasisMatchesField checks that the polynomial-basis product equals
// Leopard's GF(2^16) multiply over the whole field, and that the Cantor<->poly
// maps round-trip. This is architecture-independent and runs on x86 CI too.
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

// TestComputeGoMatchesScalar checks the pure-Go polynomial twin against the
// per-row scalar reference across eligible shapes and worker counts. It runs on
// every architecture, so x86 CI exercises the polynomial maths and the twins.
func TestComputeGoMatchesScalar(t *testing.T) {
	cases := []struct{ k, rowSize int }{
		{1, 64},
		{3, 128},
		{17, 128},
		{32, 64},
		{32, 128},
		{64, 64},
		{100, 256},
		{256, 256},
		{1023, 1024},
		{1024, 8192},
		{4096, 4096},
	}
	for _, tc := range cases {
		rows, coeffs := randomRLCInputs(tc.k, tc.rowSize)
		want := make(Vector, tc.k)
		for i, row := range rows {
			want[i] = ComputeRow(row, coeffs)
		}
		for _, workers := range []int{1, 4} {
			got := computeGo(rows, coeffs, workers)
			if len(got) != len(want) {
				t.Fatalf("k=%d rs=%d workers=%d length mismatch", tc.k, tc.rowSize, workers)
			}
			for i := range want {
				if !field.Equal128(want[i], got[i]) {
					t.Fatalf("k=%d rs=%d workers=%d row %d: twin %v != scalar %v",
						tc.k, tc.rowSize, workers, i, got[i], want[i])
				}
			}
		}
	}
}

// TestRequireASM logs which RLC compute path is in use. When RSEMA1D_REQUIRE_ASM=1
// it FAILS (does not skip) if an asm-capable arm64 build is not actually running
// the PMULL path, so a runner-image or CPU regression cannot silently drop asm
// coverage in CI.
func TestRequireASM(t *testing.T) {
	var path string
	switch {
	case !asmBuild:
		path = "portable (assembly excluded by GOARCH or build tags)"
	case asmPathActive():
		path = "arm64 PMULL"
	default:
		path = "portable (arm64 CPU lacks PMULL/SHA3)"
	}
	t.Logf("RLC compute path: %s (GOARCH=%s)", path, runtime.GOARCH)

	if os.Getenv("RSEMA1D_REQUIRE_ASM") != "1" {
		return
	}
	switch {
	case runtime.GOARCH != "arm64":
		t.Logf("RSEMA1D_REQUIRE_ASM=1 not enforced on GOARCH=%s", runtime.GOARCH)
	case !asmBuild:
		t.Logf("RSEMA1D_REQUIRE_ASM=1 not enforced: assembly excluded by build tags (-race/noasm/nopshufb)")
	case !asmPathActive():
		t.Fatal("RSEMA1D_REQUIRE_ASM=1 but the PMULL RLC path is inactive on this arm64 CPU (missing PMULL/SHA3?)")
	}
}

// FuzzCompute compares the dispatched Compute, the pure-Go twin computeGo, and
// the scalar ComputeRow for random K, valid row lengths, contents, coefficients,
// worker counts and unaligned buffers. The seed corpus runs under normal
// `go test`.
func FuzzCompute(f *testing.F) {
	f.Add(uint16(1), uint8(1), uint8(1), uint64(1))
	f.Add(uint16(3), uint8(2), uint8(4), uint64(7))
	f.Add(uint16(17), uint8(1), uint8(16), uint64(99))
	f.Add(uint16(32), uint8(4), uint8(2), uint64(1234))
	f.Add(uint16(200), uint8(16), uint8(8), uint64(55555))
	f.Fuzz(func(t *testing.T, kRaw uint16, chunksRaw, workersRaw uint8, seed uint64) {
		k := int(kRaw%256) + 1
		numChunks := int(chunksRaw%16) + 1
		rowSize := numChunks * chunkSize
		workers := int(workersRaw%16) + 1

		r := rand.New(rand.NewPCG(seed, uint64(kRaw)<<24^uint64(chunksRaw)<<16^uint64(workersRaw)))
		rows := make([][]byte, k)
		for i := range rows {
			// Force an unaligned start to exercise buffer handling.
			buf := make([]byte, rowSize+1)
			for j := range buf {
				buf[j] = byte(r.Uint32())
			}
			rows[i] = buf[1:]
		}
		coeffs := make(Vector, rowSize/2)
		for i := range coeffs {
			for c := range coeffs[i] {
				coeffs[i][c] = uint16(r.Uint32())
			}
		}

		disp := Compute(rows, coeffs, workers)
		ref := computeGo(rows, coeffs, workers)
		if len(disp) != k || len(ref) != k {
			t.Fatalf("length mismatch: disp=%d ref=%d k=%d", len(disp), len(ref), k)
		}
		for i := range rows {
			scalar := ComputeRow(rows[i], coeffs)
			if !field.Equal128(disp[i], scalar) {
				t.Fatalf("Compute != ComputeRow: k=%d rowSize=%d workers=%d row=%d", k, rowSize, workers, i)
			}
			if !field.Equal128(ref[i], scalar) {
				t.Fatalf("computeGo != ComputeRow: k=%d rowSize=%d workers=%d row=%d", k, rowSize, workers, i)
			}
		}
	})
}

// randomRLCInputs builds k unaligned random rows of rowSize bytes and matching
// random coefficients (rowSize/2 of them).
func randomRLCInputs(k, rowSize int) ([][]byte, Vector) {
	r := rand.New(rand.NewPCG(uint64(k), uint64(rowSize)))
	rows := make([][]byte, k)
	for i := range rows {
		buf := make([]byte, rowSize+1)
		for j := range buf {
			buf[j] = byte(r.Uint32())
		}
		rows[i] = buf[1:]
	}
	coeffs := make(Vector, rowSize/2)
	for i := range coeffs {
		for c := range coeffs[i] {
			coeffs[i][c] = uint16(r.Uint32())
		}
	}
	return rows, coeffs
}
