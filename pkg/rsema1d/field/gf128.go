package field

import (
	"encoding/binary"

	"github.com/klauspost/reedsolomon"
)

const (
	// GF128Width is the number of GF(2^16) components that make up one GF128 element.
	GF128Width = 8
	// GF128Size is the byte size of a serialized GF128 element.
	GF128Size = GF128Width * 2
)

// GF128 represents GF(2^128) as an 8-dimensional vector over GF(2^16).
type GF128 [GF128Width]uint16

// Zero returns the zero element in GF128.
func Zero() GF128 {
	return GF128{}
}

// Add128 adds two GF128 elements (component-wise XOR).
func Add128(a, b GF128) GF128 {
	var result GF128
	for i := range GF128Width {
		result[i] = a[i] ^ b[i]
	}
	return result
}

// Mul128 multiplies a GF(2^16) scalar with a GF128 vector. Addition in
// GF(2^16) is plain XOR; the GF(2^16) product is delegated to reedsolomon's
// LowLevel kernel.
func Mul128(scalar uint16, vec GF128) GF128 {
	var result GF128
	for i := range GF128Width {
		result[i] = ll.GF16Mul(scalar, vec[i])
	}
	return result
}

// EncodeGF128 serializes a GF128 into dst as GF128Size little-endian bytes.
// dst must be at least GF128Size bytes.
func EncodeGF128(dst []byte, g GF128) {
	_ = dst[GF128Size-1]
	for i := range GF128Width {
		binary.LittleEndian.PutUint16(dst[i*2:], g[i])
	}
}

// DecodeGF128 deserializes the first GF128Size bytes of src to a GF128.
// src must be at least GF128Size bytes.
func DecodeGF128(src []byte) GF128 {
	_ = src[GF128Size-1]
	var g GF128
	for i := range GF128Width {
		g[i] = binary.LittleEndian.Uint16(src[i*2:])
	}
	return g
}

// Equal128 reports whether two GF128 values are equal.
func Equal128(a, b GF128) bool {
	for i := range GF128Width {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// HashToGF128 converts a 32-byte hash to a GF128 element. The two halves of
// the hash are XOR-combined component-wise for better randomness distribution.
func HashToGF128(data [32]byte) GF128 {
	var firstHalf GF128
	for i := range GF128Width {
		firstHalf[i] = binary.LittleEndian.Uint16(data[i*2:])
	}

	var secondHalf GF128
	for i := range GF128Width {
		secondHalf[i] = binary.LittleEndian.Uint16(data[16+i*2:])
	}

	return Add128(firstHalf, secondHalf)
}

// ll is the reedsolomon LowLevel handle backing the GF(2^16) primitive used
// by Mul128.
var ll reedsolomon.LowLevel
