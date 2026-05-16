package field

import (
	"encoding/binary"
	"fmt"
)

// GF128 represents GF(2^128) as 8-dimensional vector over GF16
type GF128 [8]GF16

// GF128Width is the number of GF16 components that make up one GF128 element.
const GF128Width = 8

// GF128Size is the byte size of a serialized GF128 element.
const GF128Size = GF128Width * 2

// Zero returns the zero element in GF128
func Zero() GF128 {
	return GF128{}
}

// Add128 adds two GF128 elements (component-wise XOR)
func Add128(a, b GF128) GF128 {
	var result GF128
	for i := range 8 {
		result[i] = a[i] ^ b[i]
	}
	return result
}

// Mul128 multiplies a GF16 scalar with a GF128 vector
func Mul128(scalar GF16, vec GF128) GF128 {
	var result GF128
	for i := range 8 {
		result[i] = Mul16(scalar, vec[i])
	}
	return result
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

// EncodeGF128s serializes values into dst as contiguous GF128Size-byte elements.
// dst must be at least len(values)*GF128Size bytes.
func EncodeGF128s(dst []byte, values []GF128) {
	if len(values) == 0 {
		return
	}
	_ = dst[len(values)*GF128Size-1]
	for i, value := range values {
		EncodeGF128(dst[i*GF128Size:(i+1)*GF128Size], value)
	}
}

// DecodeGF128s deserializes src into dst as contiguous GF128Size-byte elements.
// len(src) must equal len(dst)*GF128Size.
func DecodeGF128s(dst []GF128, src []byte) error {
	expectedLen := len(dst) * GF128Size
	if len(src) != expectedLen {
		return fmt.Errorf("expected %d bytes for %d GF128 values, got %d", expectedLen, len(dst), len(src))
	}
	for i := range dst {
		dst[i] = DecodeGF128(src[i*GF128Size : (i+1)*GF128Size])
	}
	return nil
}

// MarshalGF128s serializes values as contiguous GF128Size-byte elements.
func MarshalGF128s(values []GF128) []byte {
	out := make([]byte, len(values)*GF128Size)
	EncodeGF128s(out, values)
	return out
}

// UnmarshalGF128s parses src as contiguous GF128Size-byte elements.
func UnmarshalGF128s(src []byte) ([]GF128, error) {
	if len(src)%GF128Size != 0 {
		return nil, fmt.Errorf("GF128 byte length must be a multiple of %d, got %d", GF128Size, len(src))
	}
	values := make([]GF128, len(src)/GF128Size)
	if err := DecodeGF128s(values, src); err != nil {
		return nil, err
	}
	return values, nil
}

// Equal checks if two GF128 values are equal
func Equal128(a, b GF128) bool {
	for i := range 8 {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// HashToGF128 converts a 32-byte hash to a GF128 element
// XORs the two halves for better randomness distribution
func HashToGF128(data []byte) GF128 {
	if len(data) < 32 {
		panic("HashToGF128 requires at least 32 bytes")
	}

	// Take first half as 8 little-endian uint16 values
	var firstHalf GF128
	for i := range 8 {
		firstHalf[i] = GF16(binary.LittleEndian.Uint16(data[i*2:]))
	}

	// Take second half as 8 little-endian uint16 values
	var secondHalf GF128
	for i := range 8 {
		secondHalf[i] = GF16(binary.LittleEndian.Uint16(data[16+i*2:]))
	}

	// XOR the two halves for final result
	return Add128(firstHalf, secondHalf)
}
