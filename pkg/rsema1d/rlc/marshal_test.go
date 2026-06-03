package rlc_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
)

// TestMarshalRoundTrip verifies Marshal/Unmarshal round-trip correctness
// across a small spread of values and rejects truncated input.
func TestMarshalRoundTrip(t *testing.T) {
	values := rlc.Vector{
		field.Zero(),
		{1, 2, 3, 4, 5, 6, 7, 8},
		{0x1234, 0x5678, 0x9ABC, 0xDEF0, 0x1111, 0x2222, 0x3333, 0x4444},
	}

	serialized := rlc.Marshal(values)
	if got, want := len(serialized), len(values)*field.GF128Size; got != want {
		t.Fatalf("Marshal returned %d bytes, want %d", got, want)
	}

	roundTrip, err := rlc.Unmarshal(serialized)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(roundTrip) != len(values) {
		t.Fatalf("Unmarshal returned %d values, want %d", len(roundTrip), len(values))
	}
	for i := range values {
		if !field.Equal128(roundTrip[i], values[i]) {
			t.Fatalf("value %d mismatch: got %v want %v", i, roundTrip[i], values[i])
		}
	}

	if _, err := rlc.Unmarshal(serialized[:len(serialized)-1]); err == nil {
		t.Fatalf("Unmarshal accepted truncated data")
	}
}

// TestEncodeDecodeRoundTrip exercises the pre-allocated-buffer variants
// directly and rejects mismatched lengths.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	values := rlc.Vector{
		field.Zero(),
		{1, 2, 3, 4, 5, 6, 7, 8},
		{0x1234, 0x5678, 0x9ABC, 0xDEF0, 0x1111, 0x2222, 0x3333, 0x4444},
	}

	dst := make([]byte, len(values)*field.GF128Size)
	rlc.Encode(dst, values)

	decoded := make(rlc.Vector, len(values))
	if err := rlc.Decode(decoded, dst); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for i := range values {
		if !field.Equal128(decoded[i], values[i]) {
			t.Fatalf("value %d mismatch: got %v want %v", i, decoded[i], values[i])
		}
	}

	if err := rlc.Decode(decoded, dst[:len(dst)-1]); err == nil {
		t.Fatalf("Decode accepted truncated data")
	}
}
