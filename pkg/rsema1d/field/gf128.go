package field

import "encoding/binary"

// GF128 represents GF(2^128) as 8-dimensional vector over GF16
type GF128 [8]GF16

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

// ToBytes128 serializes a GF128 to 16 bytes (little-endian)
func ToBytes128(g GF128) [16]byte {
	var b [16]byte
	for i := range 8 {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(g[i]))
	}
	return b
}

// FromBytes128 deserializes 16 bytes to a GF128 (little-endian)
func FromBytes128(b [16]byte) GF128 {
	var g GF128
	for i := range 8 {
		g[i] = GF16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return g
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
