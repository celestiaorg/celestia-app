package field

import (
	"github.com/klauspost/reedsolomon"
)

// GF16 represents a GF(2^16) field element
type GF16 uint16

var ll reedsolomon.LowLevel

// Mul16 multiplies two GF(2^16) elements
func Mul16(a, b GF16) GF16 {
	return GF16(ll.GF16Mul(uint16(a), uint16(b)))
}

// Add16 adds two GF(2^16) elements (XOR)
func Add16(a, b GF16) GF16 {
	return a ^ b
}
