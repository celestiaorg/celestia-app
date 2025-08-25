package field

import (
	"bytes"
	"testing"
)

func TestGF128Zero(t *testing.T) {
	z := Zero()
	for i := 0; i < 8; i++ {
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

func TestGF128ScalarMultiplication(t *testing.T) {
	tests := []struct {
		name   string
		scalar GF16
		vec    GF128
	}{
		{
			name:   "multiply_by_zero",
			scalar: 0,
			vec:    GF128{1, 2, 3, 4, 5, 6, 7, 8},
		},
		{
			name:   "multiply_by_one",
			scalar: 1,
			vec:    GF128{1, 2, 3, 4, 5, 6, 7, 8},
		},
		{
			name:   "multiply_by_two",
			scalar: 2,
			vec:    GF128{1, 2, 3, 4, 5, 6, 7, 8},
		},
		{
			name:   "general_case",
			scalar: 0x1234,
			vec:    GF128{0xABCD, 0xEF01, 0x2345, 0x6789, 0xBCDE, 0xF012, 0x3456, 0x789A},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Mul128(tt.scalar, tt.vec)

			// Check zero property
			if tt.scalar == 0 {
				if !Equal128(result, Zero()) {
					t.Errorf("Mul128(0, %v) = %v, expected zero", tt.vec, result)
				}
			}

			// Check identity property
			if tt.scalar == 1 {
				if !Equal128(result, tt.vec) {
					t.Errorf("Mul128(1, %v) = %v, expected %v", tt.vec, result, tt.vec)
				}
			}

			// Check that each component is multiplied correctly
			for i := 0; i < 8; i++ {
				expected := Mul16(tt.scalar, tt.vec[i])
				if result[i] != expected {
					t.Errorf("Component %d: got %d, expected %d", i, result[i], expected)
				}
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
		serialized := ToBytes128(original)

		// Check size
		if len(serialized) != 16 {
			t.Errorf("Test %d: ToBytes128 returned %d bytes, expected 16", i, len(serialized))
			continue
		}

		// Deserialize back
		deserialized := FromBytes128(serialized)

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
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "all_zeros",
			data: make([]byte, 32),
		},
		{
			name: "all_ones",
			data: bytes.Repeat([]byte{0xFF}, 32),
		},
		{
			name: "sequential",
			data: func() []byte {
				b := make([]byte, 32)
				for i := range b {
					b[i] = byte(i)
				}
				return b
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HashToGF128(tt.data)

			// Test determinism - same input should give same output
			result2 := HashToGF128(tt.data)
			if !Equal128(result, result2) {
				t.Errorf("HashToGF128 not deterministic: %v != %v", result, result2)
			}

			// Test that changing input changes output
			modifiedData := make([]byte, 32)
			copy(modifiedData, tt.data)
			modifiedData[0] ^= 1
			result3 := HashToGF128(modifiedData)
			if Equal128(result, result3) {
				t.Errorf("HashToGF128 did not change with different input")
			}
		})
	}
}

func TestHashToGF128Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("HashToGF128 should panic with less than 32 bytes")
		}
	}()

	// Should panic with less than 32 bytes
	HashToGF128(make([]byte, 31))
}

func TestGF128Properties(t *testing.T) {
	// Test associativity of addition
	a := GF128{1, 2, 3, 4, 5, 6, 7, 8}
	b := GF128{9, 10, 11, 12, 13, 14, 15, 16}
	c := GF128{17, 18, 19, 20, 21, 22, 23, 24}

	// (a + b) + c = a + (b + c)
	left := Add128(Add128(a, b), c)
	right := Add128(a, Add128(b, c))
	if !Equal128(left, right) {
		t.Errorf("Addition not associative: %v != %v", left, right)
	}

	// Test distributivity of scalar multiplication
	// s * (a + b) = (s * a) + (s * b)
	scalar := GF16(0x1234)
	sum := Add128(a, b)
	left2 := Mul128(scalar, sum)
	right2 := Add128(Mul128(scalar, a), Mul128(scalar, b))
	if !Equal128(left2, right2) {
		t.Errorf("Scalar multiplication not distributive: %v != %v", left2, right2)
	}
}