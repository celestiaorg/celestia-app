//go:build arm64 && !noasm && !nopshufb && !race

package rlc

import (
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/field"
	"golang.org/x/sys/cpu"
)

// asmBuild reports that this build can take the PMULL assembly path. It is false
// under -race, -tags noasm/nopshufb, and on non-arm64 (see poly_generic.go).
const asmBuild = true

// usePoly gates the assembly path on the CPU features it needs: PMULL for the
// carry-less multiplies and SHA3 for the EOR3 used in the accumulate kernel.
var usePoly = cpu.ARM64.HasASIMD && cpu.ARM64.HasPMULL && cpu.ARM64.HasSHA3

// asmPathActive reports whether Compute will run the PMULL kernels at runtime.
func asmPathActive() bool { return usePoly }

// computeFast computes the RLC of each row with the PMULL kernels. It reports
// false when the CPU lacks PMULL+SHA3, so Compute falls back to the portable
// path.
func computeFast(rows [][]byte, coeffs Vector, workers int) (Vector, bool) {
	if !usePoly {
		return nil, false
	}
	return computePoly(rows, coeffs, workers, polyRowNEON), true
}

// polyRowNEON computes one row's RLC with the PMULL assembly kernels, using a as
// scratch for the converted row. It is the arm64 twin of polyRowGo.
func polyRowNEON(row, a []byte, tables *[2][]byte) field.GF128 {
	polyConvertNEON(a, row, &toPolyNibbles)
	var acc [2][12][16]byte
	polyAccum4NEON(&acc[0], a, tables[0])
	polyAccum4NEON(&acc[1], a, tables[1])
	return combinePolyRow(&acc)
}

// polyConvertNEON converts Leopard-layout row src to the polynomial basis,
// writing 2*polyOperandBytes to dst per 64-byte chunk. It is the assembly twin
// of polyConvertGo.
//
//go:noescape
func polyConvertNEON(dst, src []byte, tables *[128]byte)

// polyAccum4NEON XORs into acc, for four components, the Karatsuba parts of the
// carry-less products of a's operands with b's coefficients. It is the assembly
// twin of polyAccumGo.
//
//go:noescape
func polyAccum4NEON(acc *[12][16]byte, a, b []byte)
