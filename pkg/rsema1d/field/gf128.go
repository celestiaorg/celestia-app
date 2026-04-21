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

// GF128Width is the number of GF16 components that make up one GF128 element.
const GF128Width = 8

// LeopardGF128BufSize returns the byte length of a buffer that holds k GF128
// values laid out as GF128Width concatenated Leopard-formatted regions
// (one per GF128 component).
func LeopardGF128BufSize(k int) int { return GF128Width * 2 * k }

// LeopardGF128Views partitions a GF128Width-by-k Leopard-formatted byte
// buffer into GF128Width per-component slice headers. len(buf) must equal
// LeopardGF128BufSize(k). The views share the underlying buffer and are
// suitable as MulSliceXor8 destinations; recover the GF128s with
// GF128sFromLeopard(buf, k). The returned array stays on the caller's
// stack — no heap allocation.
func LeopardGF128Views(buf []byte, k int) [GF128Width][]byte {
	stride := 2 * k
	var views [GF128Width][]byte
	for i := range views {
		views[i] = buf[i*stride : (i+1)*stride]
	}
	return views
}

// GF128sFromLeopard reads a GF128Width-by-k Leopard-formatted byte buffer
// into a freshly allocated []GF128 of length k, one GF128 value per row.
// The only heap allocation is the returned slice itself.
func GF128sFromLeopard(buf []byte, k int) []GF128 {
	out := make([]GF128, k)
	stride := 2 * k
	for comp := range GF128Width {
		view := buf[comp*stride : (comp+1)*stride]
		for r := range k {
			out[r][comp] = GF16FromLeopard(view, r)
		}
	}
	return out
}
