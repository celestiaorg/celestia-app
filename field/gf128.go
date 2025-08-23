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
	for i := 0; i < 8; i++ {
		result[i] = a[i] ^ b[i]
	}
	return result
}

// Mul128 multiplies a GF16 scalar with a GF128 vector
func Mul128(scalar GF16, vec GF128) GF128 {
	var result GF128
	for i := 0; i < 8; i++ {
		result[i] = Mul16(scalar, vec[i])
	}
	return result
}

// ToBytes128 serializes a GF128 to 16 bytes (little-endian)
func ToBytes128(g GF128) [16]byte {
	var b [16]byte
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(g[i]))
	}
	return b
}

// FromBytes128 deserializes 16 bytes to a GF128 (little-endian)
func FromBytes128(b [16]byte) GF128 {
	var g GF128
	for i := 0; i < 8; i++ {
		g[i] = GF16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return g
}

// ExpandToGF128 expands 32 bytes to a GF128 element
// Takes first 16 bytes and interprets as 8 little-endian uint16 values
func ExpandToGF128(data []byte) GF128 {
	if len(data) < 16 {
		panic("ExpandToGF128 requires at least 16 bytes")
	}
	var b [16]byte
	copy(b[:], data[:16])
	return FromBytes128(b)
}