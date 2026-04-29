package rsema1d

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

func TestDeriveCoefficients(t *testing.T) {
	configs := []struct {
		name    string
		k       int
		n       int
		rowSize int
	}{
		{"small", 4, 4, 64},
		{"medium", 8, 8, 128},
		{"large", 16, 16, 256},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{
				K:           tc.k,
				N:           tc.n,
				RowSize:     tc.rowSize,
				WorkerCount: 1,
			}

			// Create a test rowRoot
			rowRoot := sha256.Sum256([]byte("test root"))

			// Derive coefficients
			coeffs1 := deriveCoefficients(rowRoot, config)
			coeffs2 := deriveCoefficients(rowRoot, config)

			// Test determinism
			if len(coeffs1) != len(coeffs2) {
				t.Fatalf("Coefficient lengths differ: %d vs %d", len(coeffs1), len(coeffs2))
			}

			for i := range coeffs1 {
				if !field.Equal128(coeffs1[i], coeffs2[i]) {
					t.Errorf("Coefficient %d not deterministic", i)
				}
			}

			// Test expected number of coefficients
			expectedNumCoeffs := config.RowSize / 2 // Each GF16 symbol is 2 bytes
			if len(coeffs1) != expectedNumCoeffs {
				t.Errorf("Got %d coefficients, expected %d", len(coeffs1), expectedNumCoeffs)
			}

			// Test that different roots produce different coefficients
			differentRoot := sha256.Sum256([]byte("different root"))
			coeffs3 := deriveCoefficients(differentRoot, config)

			allSame := true
			for i := range coeffs1 {
				if !field.Equal128(coeffs1[i], coeffs3[i]) {
					allSame = false
					break
				}
			}
			if allSame {
				t.Error("Different roots produced identical coefficients")
			}
		})
	}
}

func TestComputeRLC(t *testing.T) {
	tests := []struct {
		name    string
		rowSize int
	}{
		{"single_chunk", 64},
		{"two_chunks", 128},
		{"four_chunks", 256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				K:           4,
				N:           4,
				RowSize:     tt.rowSize,
				WorkerCount: 1,
			}

			// Create test row data
			row := make([]byte, tt.rowSize)
			for i := range row {
				row[i] = byte(i % 256)
			}

			// Derive coefficients
			rowRoot := sha256.Sum256([]byte("test"))
			coeffs := deriveCoefficients(rowRoot, config)

			// Compute RLC
			rlc1 := computeRLC(row, coeffs)
			rlc2 := computeRLC(row, coeffs)

			// Test determinism
			if !field.Equal128(rlc1, rlc2) {
				t.Error("computeRLC is not deterministic")
			}

			// Test that zero row produces zero RLC
			zeroRow := make([]byte, tt.rowSize)
			zeroRLC := computeRLC(zeroRow, coeffs)
			if !field.Equal128(zeroRLC, field.Zero()) {
				t.Error("Zero row should produce zero RLC")
			}

			// Test that different data produces different RLC
			row2 := make([]byte, tt.rowSize)
			copy(row2, row)
			row2[0] ^= 1
			rlc3 := computeRLC(row2, coeffs)
			if field.Equal128(rlc1, rlc3) {
				t.Error("Different rows produced same RLC")
			}
		})
	}
}

func TestExtractSymbols(t *testing.T) {
	// Test that extractSymbols correctly interprets Leopard format
	chunk := make([]byte, 64)

	// Set up test data in Leopard format:
	// bytes 0-31: low bytes of symbols
	// bytes 32-63: high bytes of symbols
	for i := range 32 {
		chunk[i] = byte(i * 2)      // Low byte
		chunk[32+i] = byte(i*2 + 1) // High byte
	}

	symbols := extractSymbols(chunk)

	// Verify we got 32 symbols
	if len(symbols) != 32 {
		t.Fatalf("extractSymbols returned %d symbols, expected 32", len(symbols))
	}

	// Verify each symbol is correctly formed
	for i := range 32 {
		expectedSymbol := field.GF16((i*2+1)<<8 | (i * 2))
		if symbols[i] != expectedSymbol {
			t.Errorf("Symbol %d: got %04x, expected %04x", i, symbols[i], expectedSymbol)
		}
	}
}

func TestExtractSymbolsPanic(t *testing.T) {
	// Test that extractSymbols panics with wrong size chunk
	defer func() {
		if r := recover(); r == nil {
			t.Error("extractSymbols should panic with non-64-byte chunk")
		}
	}()

	chunk := make([]byte, 63) // Wrong size
	extractSymbols(chunk)
}

func TestRLCLinearity(t *testing.T) {
	// Test that RLC is linear: RLC(a + b) = RLC(a) + RLC(b)
	config := &Config{
		K:           4,
		N:           4,
		RowSize:     64,
		WorkerCount: 1,
	}

	// Create test data
	rowA := make([]byte, 64)
	rowB := make([]byte, 64)
	rowSum := make([]byte, 64)

	for i := range rowA {
		rowA[i] = byte(i)
		rowB[i] = byte(i * 2)
		rowSum[i] = rowA[i] ^ rowB[i] // Addition in GF is XOR
	}

	// Derive coefficients
	rowRoot := sha256.Sum256([]byte("test"))
	coeffs := deriveCoefficients(rowRoot, config)

	// Compute RLCs
	rlcA := computeRLC(rowA, coeffs)
	rlcB := computeRLC(rowB, coeffs)
	rlcSum := computeRLC(rowSum, coeffs)

	// Check linearity: RLC(A + B) = RLC(A) + RLC(B)
	expectedSum := field.Add128(rlcA, rlcB)
	if !field.Equal128(rlcSum, expectedSum) {
		t.Error("RLC is not linear")
	}
}

func TestCommitmentDeterminism(t *testing.T) {
	// Test that the same data always produces the same commitment
	config := &Config{
		K:           4,
		N:           4,
		RowSize:     64,
		WorkerCount: 1,
	}

	// Create test data
	data := makeTestData(4, 64)

	// Generate commitments multiple times
	_, commitment1, _, err := Encode(data, config)
	if err != nil {
		t.Fatalf("First Encode failed: %v", err)
	}

	_, commitment2, _, err := Encode(data, config)
	if err != nil {
		t.Fatalf("Second Encode failed: %v", err)
	}

	// Commitments should be identical
	if !bytes.Equal(commitment1[:], commitment2[:]) {
		t.Error("Encode is not deterministic")
	}

	// Modify data slightly
	data[0][0] ^= 1

	_, commitment3, _, err := Encode(data, config)
	if err != nil {
		t.Fatalf("Third Encode failed: %v", err)
	}

	// Modified data should produce different commitment
	if bytes.Equal(commitment1[:], commitment3[:]) {
		t.Error("Different data produced same commitment")
	}
}

func TestCoefficientsConsistency(t *testing.T) {
	// Coefficient derivation must be deterministic for a given
	// (rowRoot, K, N, RowSize) tuple, and must differ when any of those
	// transcript inputs change.
	rowRoot := sha256.Sum256([]byte("consistent root"))
	base := &Config{K: 8, N: 8, RowSize: 128, WorkerCount: 1}

	baseCoeffs := deriveCoefficients(rowRoot, base)

	// Same inputs → identical output.
	again := deriveCoefficients(rowRoot, base)
	if len(again) != len(baseCoeffs) {
		t.Fatalf("deterministic: length mismatch: got %d want %d", len(again), len(baseCoeffs))
	}
	for i := range baseCoeffs {
		if !field.Equal128(baseCoeffs[i], again[i]) {
			t.Fatalf("deterministic: coefficient %d differs", i)
		}
	}

	// Varying any of K, N, RowSize must change the coefficients.
	variants := []struct {
		name   string
		config *Config
	}{
		{"different K", &Config{K: 4, N: 8, RowSize: 128, WorkerCount: 1}},
		{"different N", &Config{K: 8, N: 16, RowSize: 128, WorkerCount: 1}},
		{"different RowSize", &Config{K: 8, N: 8, RowSize: 256, WorkerCount: 1}},
	}
	for _, v := range variants {
		got := deriveCoefficients(rowRoot, v.config)
		if equalGF128Slice(got, baseCoeffs) {
			t.Errorf("%s: coefficients unexpectedly equal to base", v.name)
		}
	}
}

func equalGF128Slice(a, b []field.GF128) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !field.Equal128(a[i], b[i]) {
			return false
		}
	}
	return true
}
