package rlc

import (
	"math/bits"
	"sync"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/field"
)

// The RLC of a row is Σ_i s_i·c_i over Leopard's GF(2^16), whose elements are
// stored in a Cantor basis. Mapping both operands to the polynomial basis turns
// each product into a carry-less multiply, so a whole row is summed with
// unreduced carry-less products and reduced once per component.
//
// Everything in this file is architecture-independent: the basis tables, the
// basis maps, coefficient packing, and pure-Go twins of the two arm64 kernels
// (polyConvertGo for polyConvertNEON, polyAccumGo for polyAccum4NEON). The twins
// are the reference the assembly is checked against bit for bit on arm64, and
// the path x86 CI compiles and tests against ComputeRow. Compute dispatches to
// the assembly, never to the twins.

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

// computePoly computes the RLC of every row with the polynomial kernels. rowFn
// runs one row (the assembly on arm64, polyRowGo in the Go twin path). Rows must
// be Leopard-sized; coeffs must have at least len(rows[0])/2 entries.
func computePoly(rows [][]byte, coeffs Vector, workers int, rowFn func(row, a []byte, tables *[2][]byte) field.GF128) Vector {
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
			out[r] = rowFn(rows[r][:rowSize], a, &tables)
		}
	})
	return out
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

// combinePolyRow folds the carry-less accumulators of one row into a GF128.
// acc[k/4] holds the four components of half-table k/4; within it the Karatsuba
// low/high/mid parts of component k%4 are collapsed, reduced once and mapped
// back to the Cantor basis.
func combinePolyRow(acc *[2][12][16]byte) field.GF128 {
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

// polyRowGo computes one row's RLC with the pure-Go twins. It is used by
// computeGo and is the reference the assembly is validated against.
func polyRowGo(row, a []byte, tables *[2][]byte) field.GF128 {
	polyConvertGo(a, row, &toPolyNibbles)
	var acc [2][12][16]byte
	polyAccumGo(&acc[0], a, tables[0])
	polyAccumGo(&acc[1], a, tables[1])
	return combinePolyRow(&acc)
}

// computeGo computes the RLC of every row with the pure-Go polynomial twins. It
// is the architecture-independent reference the assembly is checked against;
// Compute never dispatches to it.
func computeGo(rows [][]byte, coeffs Vector, workers int) Vector {
	return computePoly(rows, coeffs, workers, polyRowGo)
}

// clmulByte returns the carry-less product of two bytes.
func clmulByte(x, y byte) uint16 {
	var p uint16
	for i := range 8 {
		if y>>i&1 == 1 {
			p ^= uint16(x) << i
		}
	}
	return p
}

// polyConvertGo is the pure-Go twin of polyConvertNEON. It maps Leopard-layout
// row src to the polynomial basis, writing polyOperandBytes per 16-symbol block
// (16 low bytes, then 16 high bytes, then their XOR for Karatsuba). src is
// consumed in 64-byte Leopard chunks of 32 symbols. tables is toPolyNibbles.
func polyConvertGo(dst, src []byte, tables *[128]byte) {
	di := 0
	for off := 0; off+chunkSize <= len(src); off += chunkSize {
		los := src[off : off+symbolsPerChunk]
		his := src[off+symbolsPerChunk : off+chunkSize]
		for blk := range 2 {
			base := blk * polyBlockSymbols
			for j := range polyBlockSymbols {
				l, h := los[base+j], his[base+j]
				outLo := tables[l&0xf] ^ tables[16+l>>4] ^ tables[32+h&0xf] ^ tables[48+h>>4]
				outHi := tables[64+l&0xf] ^ tables[80+l>>4] ^ tables[96+h&0xf] ^ tables[112+h>>4]
				dst[di+j] = outLo
				dst[di+polyBlockSymbols+j] = outHi
				dst[di+2*polyBlockSymbols+j] = outLo ^ outHi
			}
			di += polyOperandBytes
		}
	}
}

// polyAccumGo is the pure-Go twin of polyAccum4NEON. For four GF128 components
// it XOR-accumulates, into acc, the Karatsuba parts of the carry-less products
// of a's operands with b's coefficients. a is consumed in polyOperandBytes
// blocks, b in polyCoeffBytes blocks. Each acc entry holds eight little-endian
// 16-bit lanes; lane i folds operand bytes i and i+8, as the PMULL/EOR3 pair in
// the assembly does.
func polyAccumGo(acc *[12][16]byte, a, b []byte) {
	for len(a) >= polyOperandBytes {
		parts := [3][]byte{
			a[0:polyBlockSymbols],
			a[polyBlockSymbols : 2*polyBlockSymbols],
			a[2*polyBlockSymbols : polyOperandBytes],
		}
		for idx := range 12 {
			apart := parts[idx%3]
			bpart := b[idx*16 : idx*16+16]
			for i := range 8 {
				v := clmulByte(bpart[i], apart[i]) ^ clmulByte(bpart[i+8], apart[i+8])
				cur := uint16(acc[idx][2*i]) | uint16(acc[idx][2*i+1])<<8
				cur ^= v
				acc[idx][2*i] = byte(cur)
				acc[idx][2*i+1] = byte(cur >> 8)
			}
		}
		a = a[polyOperandBytes:]
		b = b[polyCoeffBytes:]
	}
}
