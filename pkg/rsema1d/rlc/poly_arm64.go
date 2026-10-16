//go:build arm64 && !noasm && !nopshufb && !race

package rlc

import (
	"math/bits"
	"sync"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"
	"golang.org/x/sys/cpu"
)

// The RLC of a row is Σ_i s_i·c_i over Leopard's GF(2^16), whose elements are
// stored in a Cantor basis. Mapping both operands to the polynomial basis
// turns each product into a carry-less multiply, so a whole row is summed
// with unreduced PMULL products and reduced once.

var usePoly = cpu.ARM64.HasASIMD && cpu.ARM64.HasPMULL && cpu.ARM64.HasSHA3

// cantorBasis is Leopard's GF(2^16) basis in polynomial form.
var cantorBasis = [16]uint16{
	0x0001, 0xACCA, 0x3C0E, 0x163E,
	0xC582, 0xED2E, 0x914C, 0x4012,
	0x6C98, 0x10D8, 0x6A72, 0xB900,
	0xFDB8, 0xFB34, 0xFF38, 0x991E,
}

const polyModulus = 0x1002D

var (
	polyOnce sync.Once
	// toPolyLo/Hi map the low/high byte of a Cantor element to the polynomial basis.
	toPolyLo, toPolyHi [256]uint16
	// fromPolyLo/Hi are the inverse maps.
	fromPolyLo, fromPolyHi [256]uint16
	// toPolyNibbles is toPoly as NEON nibble tables: 4 tables for the low
	// output byte then 4 for the high byte, one per input nibble.
	toPolyNibbles [128]byte
)

func initPoly() {
	for x := range 256 {
		for i := range 8 {
			if x>>i&1 == 1 {
				toPolyLo[x] ^= cantorBasis[i]
				toPolyHi[x] ^= cantorBasis[8+i]
			}
		}
	}
	for v := range 1 << 16 {
		p := toPoly(uint16(v))
		if p>>8 == 0 {
			fromPolyLo[p] = uint16(v)
		}
		if p&0xff == 0 {
			fromPolyHi[p>>8] = uint16(v)
		}
	}
	for n := range 4 {
		for x := range 16 {
			p := toPoly(uint16(x << (4 * n)))
			toPolyNibbles[n*16+x] = byte(p)
			toPolyNibbles[64+n*16+x] = byte(p >> 8)
		}
	}
}

func toPoly(v uint16) uint16   { return toPolyLo[v&0xff] ^ toPolyHi[v>>8] }
func fromPoly(p uint16) uint16 { return fromPolyLo[p&0xff] ^ fromPolyHi[p>>8] }

// reducePoly reduces a carry-less product of two 16-bit polynomials.
func reducePoly(x uint32) uint16 {
	for x>>16 != 0 {
		x ^= polyModulus << (bits.Len32(x) - 17)
	}
	return uint16(x)
}

// Per 16-symbol block, an operand is stored as its low bytes, high bytes and
// their XOR (for Karatsuba). The coefficient table holds four components per
// block and half-table.
const (
	polyBlockSymbols = 16
	polyOperandBytes = 3 * polyBlockSymbols
	polyCoeffBytes   = 4 * polyOperandBytes
)

// computeFast computes the RLC of each row with PMULL. It reports false when
// the CPU lacks PMULL or EOR3.
func computeFast(rows [][]byte, coeffs Vector, workers int) (Vector, bool) {
	if !usePoly {
		return nil, false
	}
	polyOnce.Do(initPoly)
	rowSize := len(rows[0])
	blocks := rowSize / (2 * polyBlockSymbols)
	var tables [2][]byte
	tables[0] = make([]byte, blocks*polyCoeffBytes)
	tables[1] = make([]byte, blocks*polyCoeffBytes)
	parallelSpans(blocks, workers, func(lo, hi int) {
		fillPolyCoeffs(&tables, coeffs, lo, hi)
	})

	out := make(Vector, len(rows))
	parallelSpans(len(rows), workers, func(lo, hi int) {
		a := make([]byte, blocks*polyOperandBytes)
		for r := lo; r < hi; r++ {
			out[r] = polyRow(rows[r][:rowSize], a, &tables)
		}
	})
	return out, true
}

// parallelSpans runs fn over near-equal spans of [0, n) on up to workers goroutines.
func parallelSpans(n, workers int, fn func(lo, hi int)) {
	workers = min(max(workers, 1), n)
	if workers == 1 {
		fn(0, n)
		return
	}
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		lo, hi := chunkSpan(n, workers, w)
		go func() {
			defer wg.Done()
			fn(lo, hi)
		}()
	}
	wg.Wait()
}

// fillPolyCoeffs writes blocks [lo, hi) of the coefficient tables. Symbol
// 16*b+j of block b sits in lane j, matching polyConvertNEON's output.
func fillPolyCoeffs(tables *[2][]byte, coeffs Vector, lo, hi int) {
	for b := lo; b < hi; b++ {
		for j := range polyBlockSymbols {
			c := &coeffs[b*polyBlockSymbols+j]
			for k, v := range c {
				p := toPoly(v)
				off := b*polyCoeffBytes + (k%4)*polyOperandBytes + j
				t := tables[k/4]
				t[off] = byte(p)
				t[off+polyBlockSymbols] = byte(p >> 8)
				t[off+2*polyBlockSymbols] = byte(p) ^ byte(p>>8)
			}
		}
	}
}

// polyRow returns the RLC of one row, using a as scratch for the converted row.
func polyRow(row, a []byte, tables *[2][]byte) field.GF128 {
	polyConvertNEON(a, row, &toPolyNibbles)
	var acc [2][12][16]byte
	polyAccum4NEON(&acc[0], a, tables[0])
	polyAccum4NEON(&acc[1], a, tables[1])
	var g field.GF128
	for k := range field.GF128Width {
		t := &acc[k/4]
		lo := xorLanes(&t[3*(k%4)])
		hi := xorLanes(&t[3*(k%4)+1])
		mid := xorLanes(&t[3*(k%4)+2]) ^ lo ^ hi
		g[k] = fromPoly(reducePoly(uint32(lo) ^ uint32(mid)<<8 ^ uint32(hi)<<16))
	}
	return g
}

// xorLanes XORs the eight 16-bit lanes of v.
func xorLanes(v *[16]byte) uint16 {
	var x uint16
	for i := 0; i < 16; i += 2 {
		x ^= uint16(v[i]) | uint16(v[i+1])<<8
	}
	return x
}

// polyConvertNEON converts Leopard-layout row src to the polynomial basis,
// writing 2*polyOperandBytes to dst per 64-byte chunk.
//
//go:noescape
func polyConvertNEON(dst, src []byte, tables *[128]byte)

// polyAccum4NEON XORs into acc, for four components, the Karatsuba parts of
// the carry-less products of a's operands with b's coefficients.
//
//go:noescape
func polyAccum4NEON(acc *[12][16]byte, a, b []byte)
