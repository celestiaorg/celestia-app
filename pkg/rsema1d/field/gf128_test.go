package field

import (
	"testing"
)

func TestGF128Zero(t *testing.T) {
	z := Zero()
	for i := range 8 {
		if z[i] != 0 {
			t.Errorf("Zero() element %d = %d, expected 0", i, z[i])
		}
	}
}

func TestGF128Addition(t *testing.T) {
	tests := []struct {
		name string
		a, b GF128
		want GF128
	}{
		{
			name: "zero_plus_zero",
			a:    Zero(),
			b:    Zero(),
			want: Zero(),
		},
		{
			name: "identity",
			a:    GF128{1, 2, 3, 4, 5, 6, 7, 8},
			b:    Zero(),
			want: GF128{1, 2, 3, 4, 5, 6, 7, 8},
		},
		{
			name: "self_inverse",
			a:    GF128{1, 2, 3, 4, 5, 6, 7, 8},
			b:    GF128{1, 2, 3, 4, 5, 6, 7, 8},
			want: Zero(),
		},
		{
			name: "xor_operation",
			a:    GF128{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			b:    GF128{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
			want: GF128{0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Add128(tt.a, tt.b)
			if !Equal128(result, tt.want) {
				t.Errorf("Add128(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.want)
			}

			// Test commutativity
			result2 := Add128(tt.b, tt.a)
			if !Equal128(result, result2) {
				t.Errorf("Add128 not commutative: %v != %v", result, result2)
			}
		})
	}
}

func TestGF128Serialization(t *testing.T) {
	tests := []GF128{
		Zero(),
		{1, 2, 3, 4, 5, 6, 7, 8},
		{0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF},
		{0x1234, 0x5678, 0x9ABC, 0xDEF0, 0x1111, 0x2222, 0x3333, 0x4444},
	}

	for i, original := range tests {
		// Serialize to bytes
		var serialized [GF128Size]byte
		EncodeGF128(serialized[:], original)

		// Check size
		if len(serialized) != GF128Size {
			t.Errorf("Test %d: EncodeGF128 returned %d bytes, expected %d", i, len(serialized), GF128Size)
			continue
		}

		// Deserialize back
		deserialized := DecodeGF128(serialized[:])

		// Check round-trip
		if !Equal128(deserialized, original) {
			t.Errorf("Test %d: round-trip failed, got %v, expected %v", i, deserialized, original)
		}
	}
}

func TestGF128Equal(t *testing.T) {
	a := GF128{1, 2, 3, 4, 5, 6, 7, 8}
	b := GF128{1, 2, 3, 4, 5, 6, 7, 8}
	c := GF128{1, 2, 3, 4, 5, 6, 7, 9} // Different last element

	if !Equal128(a, b) {
		t.Errorf("Equal128(%v, %v) = false, expected true", a, b)
	}

	if Equal128(a, c) {
		t.Errorf("Equal128(%v, %v) = true, expected false", a, c)
	}

	if !Equal128(Zero(), Zero()) {
		t.Errorf("Equal128(Zero(), Zero()) = false, expected true")
	}
}

func TestHashToGF128(t *testing.T) {
	var sequential [32]byte
	for i := range sequential {
		sequential[i] = byte(i)
	}
	var allOnes [32]byte
	for i := range allOnes {
		allOnes[i] = 0xFF
	}
	tests := []struct {
		name string
		data [32]byte
	}{
		{"all_zeros", [32]byte{}},
		{"all_ones", allOnes},
		{"sequential", sequential},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HashToGF128(tt.data)

			// Test determinism — same input should give same output.
			result2 := HashToGF128(tt.data)
			if !Equal128(result, result2) {
				t.Errorf("HashToGF128 not deterministic: %v != %v", result, result2)
			}

			// Test that changing input changes output.
			modified := tt.data
			modified[0] ^= 1
			result3 := HashToGF128(modified)
			if Equal128(result, result3) {
				t.Errorf("HashToGF128 did not change with different input")
			}
		})
	}
}

func TestAdd128Associative(t *testing.T) {
	a := GF128{1, 2, 3, 4, 5, 6, 7, 8}
	b := GF128{9, 10, 11, 12, 13, 14, 15, 16}
	c := GF128{17, 18, 19, 20, 21, 22, 23, 24}

	if !Equal128(Add128(Add128(a, b), c), Add128(a, Add128(b, c))) {
		t.Errorf("Add128 not associative")
	}
}
