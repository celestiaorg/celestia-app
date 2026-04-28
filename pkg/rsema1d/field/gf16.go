package field

import (
	"github.com/klauspost/reedsolomon"
)

// GF16 represents a GF(2^16) field element
type GF16 uint16

// GF16FromLeopard extracts the r-th GF16 element from a byte slice laid out
// in Leopard format, i.e. one or more 64-byte chunks each containing 32 low
// bytes followed by 32 high bytes. Caller must ensure r*2 < len(slab).
func GF16FromLeopard(slab []byte, r int) GF16 {
	rb, rr := r/32, r%32
	return GF16(uint16(slab[rb*64+32+rr])<<8 | uint16(slab[rb*64+rr]))
}

var ll reedsolomon.LowLevel

// Mul16 multiplies two GF(2^16) elements
func Mul16(a, b GF16) GF16 {
	return GF16(ll.GF16Mul(uint16(a), uint16(b)))
}

// Add16 adds two GF(2^16) elements (XOR)
func Add16(a, b GF16) GF16 {
	return a ^ b
}

// MulSliceXor8 applies the 8 GF16 components of `coeff` as scalars to one
// shared input slice, XOR-accumulating each into a distinct output slice:
//
//	out[k][i] ^= coeff[k] * in[i]   for k in [0, 8)
//
// All slices must have equal length, a multiple of 64 bytes, in Leopard
// format (32 low bytes + 32 high bytes per 64-byte chunk).
func MulSliceXor8(coeff *GF128, in []byte, outs *[8][]byte) {
	var s [8]uint16
	for k, v := range coeff {
		s[k] = uint16(v)
	}
	ll.GF16MulSliceXor8(&s, in, outs)
}
