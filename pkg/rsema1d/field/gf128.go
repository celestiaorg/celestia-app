package field

import (
	"encoding/binary"
)

const (
	// GF128Width is the number of GF16 components that make up one GF128 element.
	GF128Width = 8
	// GF128Size is the byte size of a serialized GF128 element.
	GF128Size = GF128Width * 2
)

// GF128 represents GF(2^128) as 8-dimensional vector over GF16
type GF128 [GF128Width]GF16

// Zero returns the zero element in GF128
func Zero() GF128 {
	return GF128{}
}

// Add128 adds two GF128 elements (component-wise XOR)
func Add128(a, b GF128) GF128 {
	var result GF128
	for i := range GF128Width {
		result[i] = a[i] ^ b[i]
	}
	return result
}

// Mul128 multiplies a GF16 scalar with a GF128 vector
func Mul128(scalar GF16, vec GF128) GF128 {
	var result GF128
	for i := range GF128Width {
		result[i] = Mul16(scalar, vec[i])
	}
	return result
}

// ToBytes128 serializes a GF128 to GF128Size little-endian bytes.
// Used by the deprecated standalone-proof path; gets removed with proof.go.
func ToBytes128(g GF128) [GF128Size]byte {
	var out [GF128Size]byte
	EncodeGF128(out[:], g)
	return out
}

// EncodeGF128 serializes a GF128 into dst as GF128Size little-endian bytes.
// dst must be at least GF128Size bytes.
func EncodeGF128(dst []byte, g GF128) {
	_ = dst[GF128Size-1]
	for i := range GF128Width {
		binary.LittleEndian.PutUint16(dst[i*2:], uint16(g[i]))
	}
}

// DecodeGF128 deserializes the first GF128Size bytes of src to a GF128.
// src must be at least GF128Size bytes.
func DecodeGF128(src []byte) GF128 {
	_ = src[GF128Size-1]
	var g GF128
	for i := range GF128Width {
		g[i] = GF16(binary.LittleEndian.Uint16(src[i*2:]))
	}
	return g
}

// Equal checks if two GF128 values are equal
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
	// Take first half as 8 little-endian uint16 values
	var firstHalf GF128
	for i := range GF128Width {
		firstHalf[i] = GF16(binary.LittleEndian.Uint16(data[i*2:]))
	}

	// Take second half as 8 little-endian uint16 values
	var secondHalf GF128
	for i := range GF128Width {
		secondHalf[i] = GF16(binary.LittleEndian.Uint16(data[16+i*2:]))
	}

	// XOR the two halves for final result
	return Add128(firstHalf, secondHalf)
}
