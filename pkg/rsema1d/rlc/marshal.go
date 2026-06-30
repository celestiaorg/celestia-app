package rlc

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"
)

// Encode serializes values into dst as contiguous field.GF128Size-byte
// elements. dst must be at least len(values)*field.GF128Size bytes.
func Encode(dst []byte, values Vector) {
	if len(values) == 0 {
		return
	}
	_ = dst[len(values)*field.GF128Size-1]
	for i, v := range values {
		field.EncodeGF128(dst[i*field.GF128Size:(i+1)*field.GF128Size], v)
	}
}

// Decode deserializes src into dst as contiguous field.GF128Size-byte
// elements. len(src) must equal len(dst)*field.GF128Size.
func Decode(dst Vector, src []byte) error {
	expectedLen := len(dst) * field.GF128Size
	if len(src) != expectedLen {
		return fmt.Errorf("expected %d bytes for %d GF128 values, got %d", expectedLen, len(dst), len(src))
	}
	for i := range dst {
		dst[i] = field.DecodeGF128(src[i*field.GF128Size : (i+1)*field.GF128Size])
	}
	return nil
}

// Marshal serializes RLC values (coefficients or computed RLCs) as a
// contiguous byte stream of field.GF128Size-byte elements.
func Marshal(values Vector) []byte {
	out := make([]byte, len(values)*field.GF128Size)
	Encode(out, values)
	return out
}

// Unmarshal parses src as a contiguous stream of field.GF128Size-byte RLC
// values. len(src) must be a multiple of field.GF128Size.
func Unmarshal(src []byte) (Vector, error) {
	if len(src)%field.GF128Size != 0 {
		return nil, fmt.Errorf("GF128 byte length must be a multiple of %d, got %d", field.GF128Size, len(src))
	}
	values := make(Vector, len(src)/field.GF128Size)
	if err := Decode(values, src); err != nil {
		return nil, err
	}
	return values, nil
}
