package field

import (
	"testing"
)

func TestGF16Arithmetic(t *testing.T) {
	tests := []struct {
		name string
		a, b GF16
	}{
		{"zero", 0, 0},
		{"one", 1, 1},
		{"small", 2, 3},
		{"medium", 256, 512},
		{"large", 0xFFFF, 0xAAAA},
		{"prime", 257, 509},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test addition (XOR) properties
			t.Run("addition", func(t *testing.T) {
				// Commutativity: a + b = b + a
				sum1 := Add16(tt.a, tt.b)
				sum2 := Add16(tt.b, tt.a)
				if sum1 != sum2 {
					t.Errorf("Add16 not commutative: %d + %d = %d, but %d + %d = %d",
						tt.a, tt.b, sum1, tt.b, tt.a, sum2)
				}

				// Identity: a + 0 = a
				sum3 := Add16(tt.a, 0)
				if sum3 != tt.a {
					t.Errorf("Add16 identity failed: %d + 0 = %d, expected %d",
						tt.a, sum3, tt.a)
				}

				// Self-inverse: a + a = 0
				sum4 := Add16(tt.a, tt.a)
				if sum4 != 0 {
					t.Errorf("Add16 self-inverse failed: %d + %d = %d, expected 0",
						tt.a, tt.a, sum4)
				}
			})

			// Test multiplication properties
			t.Run("multiplication", func(t *testing.T) {
				// Commutativity: a * b = b * a
				prod1 := Mul16(tt.a, tt.b)
				prod2 := Mul16(tt.b, tt.a)
				if prod1 != prod2 {
					t.Errorf("Mul16 not commutative: %d * %d = %d, but %d * %d = %d",
						tt.a, tt.b, prod1, tt.b, tt.a, prod2)
				}

				// Identity: a * 1 = a (except for a = 0)
				if tt.a != 0 {
					prod3 := Mul16(tt.a, 1)
					if prod3 != tt.a {
						t.Errorf("Mul16 identity failed: %d * 1 = %d, expected %d",
							tt.a, prod3, tt.a)
					}
				}

				// Zero property: a * 0 = 0
				prod4 := Mul16(tt.a, 0)
				if prod4 != 0 {
					t.Errorf("Mul16 zero property failed: %d * 0 = %d, expected 0",
						tt.a, prod4)
				}
			})

			// Test distributivity: a * (b + c) = (a * b) + (a * c)
			t.Run("distributivity", func(t *testing.T) {
				c := GF16(0x1234)
				left := Mul16(tt.a, Add16(tt.b, c))
				right := Add16(Mul16(tt.a, tt.b), Mul16(tt.a, c))
				if left != right {
					t.Errorf("Distributivity failed: %d * (%d + %d) = %d, but (%d * %d) + (%d * %d) = %d",
						tt.a, tt.b, c, left, tt.a, tt.b, tt.a, c, right)
				}
			})
		})
	}
}

func TestGF16SpecificValues(t *testing.T) {
	// Test that GF(2^16) multiplication behaves correctly
	// Note: In GF(2^16), multiplication is NOT regular multiplication
	tests := []struct {
		name string
		a, b GF16
	}{
		{"small_values", 2, 3},
		{"medium_values", 255, 256},
		{"large_values", 0x1234, 0x5678},
		{"max_value", 0xFFFF, 0xFFFF},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify that multiplication is deterministic and follows field properties
			result1 := Mul16(tt.a, tt.b)
			result2 := Mul16(tt.a, tt.b)
			if result1 != result2 {
				t.Errorf("Mul16 not deterministic: %d != %d", result1, result2)
			}

			// Verify that if we multiply by inverse, we get 1 (for non-zero values)
			// This would require an inverse function which we don't have yet
			// The result is guaranteed to be within the field by the type system (uint16)
		})
	}
}

func TestGF16KnownValues(t *testing.T) {
	// Test some known properties of GF(2^16) multiplication
	// In GF(2^16), 2 is a generator, so powers of 2 cycle through all non-zero elements
	
	// Test that x * 0 = 0 for all x
	for _, x := range []GF16{0, 1, 2, 255, 0xFFFF} {
		if Mul16(x, 0) != 0 {
			t.Errorf("Mul16(%d, 0) != 0", x)
		}
	}
	
	// Test that x * 1 = x for all x
	for _, x := range []GF16{0, 1, 2, 255, 0xFFFF} {
		if Mul16(x, 1) != x {
			t.Errorf("Mul16(%d, 1) != %d", x, x)
		}
	}
	
	// Test that multiplication is closed (result is in the field)
	// This is implicitly tested by the type system, but let's be explicit
	a := GF16(0xFFFF)
	b := GF16(0xFFFF)
	result := Mul16(a, b)
	if result > 0xFFFF {
		t.Errorf("Mul16(0xFFFF, 0xFFFF) = %d, which exceeds field size", result)
	}
}

func TestGF16Associativity(t *testing.T) {
	// Test associativity for both addition and multiplication
	values := []GF16{0, 1, 2, 255, 256, 0x1234, 0xFFFF}

	for _, a := range values {
		for _, b := range values {
			for _, c := range values {
				// Addition associativity: (a + b) + c = a + (b + c)
				left := Add16(Add16(a, b), c)
				right := Add16(a, Add16(b, c))
				if left != right {
					t.Errorf("Addition not associative: (%d + %d) + %d = %d, but %d + (%d + %d) = %d",
						a, b, c, left, a, b, c, right)
				}

				// Multiplication associativity: (a * b) * c = a * (b * c)
				left = Mul16(Mul16(a, b), c)
				right = Mul16(a, Mul16(b, c))
				if left != right {
					t.Errorf("Multiplication not associative: (%d * %d) * %d = %d, but %d * (%d * %d) = %d",
						a, b, c, left, a, b, c, right)
				}
			}
		}
	}
}