package rsema1d

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/rsema1d/encoding"
	"github.com/celestiaorg/rsema1d/field"
)

// TestPropertyCommitmentDeterminism tests that encoding the same data always produces the same commitment
func TestPropertyCommitmentDeterminism(t *testing.T) {
	configs := []struct {
		name string
		k    int
		n    int
		rows int
	}{
		{"tiny", 4, 4, 64},
		{"small", 8, 8, 128},
		{"medium", 16, 16, 256},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{
				K:           tc.k,
				N:           tc.n,
				RowSize:     tc.rows,
				WorkerCount: 1,
			}

			// Create deterministic test data
			data := makeTestData(tc.k, tc.rows)

			// Run encoding multiple times
			const numTrials = 5
			var commitments []Commitment

			for i := 0; i < numTrials; i++ {
				_, commitment, err := Encode(data, config)
				if err != nil {
					t.Fatalf("Trial %d: Encode failed: %v", i, err)
				}
				commitments = append(commitments, commitment)
			}

			// All commitments should be identical
			for i := 1; i < numTrials; i++ {
				if !bytes.Equal(commitments[0][:], commitments[i][:]) {
					t.Errorf("Trial %d produced different commitment", i)
				}
			}
		})
	}
}

// TestPropertyProofSoundness tests that proofs only verify with correct data
func TestPropertyProofSoundness(t *testing.T) {
	config := &Config{
		K:           8,
		N:           8,
		RowSize:     128,
		WorkerCount: 1,
	}

	data := makeTestData(config.K, config.RowSize)
	extData, commitment, err := Encode(data, config)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Test multiple row indices
	testIndices := []int{0, config.K - 1, config.K, config.K + config.N - 1}

	for _, index := range testIndices {
		t.Run(fmt.Sprintf("index_%d", index), func(t *testing.T) {
			// Generate valid proof
			proof, err := extData.GenerateProof(index)
			if err != nil {
				t.Fatalf("GenerateProof failed: %v", err)
			}

			// Valid proof should verify
			if err := VerifyProof(proof, commitment, config); err != nil {
				t.Errorf("Valid proof failed to verify: %v", err)
			}

			// Test with wrong commitment
			wrongCommitment := commitment
			wrongCommitment[0] ^= 1
			if err := VerifyProof(proof, wrongCommitment, config); err == nil {
				t.Error("Proof verified with wrong commitment")
			}

			// Test with corrupted row data
			corruptedProof := *proof
			corruptedProof.Row = make([]byte, len(proof.Row))
			copy(corruptedProof.Row, proof.Row)
			corruptedProof.Row[0] ^= 1
			if err := VerifyProof(&corruptedProof, commitment, config); err == nil {
				t.Error("Proof verified with corrupted row data")
			}

			// Test with wrong index
			wrongIndexProof := *proof
			wrongIndexProof.Index = (index + 1) % (config.K + config.N)
			if err := VerifyProof(&wrongIndexProof, commitment, config); err == nil {
				t.Error("Proof verified with wrong index")
			}
		})
	}
}

// TestPropertyReconstructionCorrectness tests that reconstruction always recovers original data
func TestPropertyReconstructionCorrectness(t *testing.T) {
	configs := []struct {
		name string
		k    int
		n    int
	}{
		{"1:1_ratio", 8, 8},
		{"1:3_ratio", 4, 12},
		{"mixed", 8, 24},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{
				K:           tc.k,
				N:           tc.n,
				RowSize:     128,
				WorkerCount: 1,
			}

			// Create unique test data
			originalData := make([][]byte, tc.k)
			for i := 0; i < tc.k; i++ {
				originalData[i] = make([]byte, 128)
				for j := 0; j < 128; j++ {
					// Unique pattern for each row
					originalData[i][j] = byte((i*128 + j) % 256)
				}
			}

			extData, _, err := Encode(originalData, config)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			// Test reconstruction from various row combinations
			testCases := []struct {
				name    string
				indices []int
			}{
				{"first_k_rows", makeRange(0, tc.k)},
				{"last_k_rows", makeRange(tc.n, tc.k+tc.n)[:tc.k]},
				{"alternating", makeAlternatingIndices(tc.k, tc.n)},
				{"random_mix", makeMixedIndices(tc.k, tc.n)},
			}

			for _, test := range testCases {
				t.Run(test.name, func(t *testing.T) {
					// Select rows for reconstruction
					rows := make([][]byte, len(test.indices))
					for i, idx := range test.indices {
						rows[i] = extData.rows[idx]
					}

					// Reconstruct
					reconstructed, err := Reconstruct(rows, test.indices, config)
					if err != nil {
						t.Fatalf("Reconstruct failed: %v", err)
					}

					// Verify all rows match
					if len(reconstructed) != tc.k {
						t.Fatalf("Reconstructed %d rows, expected %d", len(reconstructed), tc.k)
					}

					for i := 0; i < tc.k; i++ {
						if !bytes.Equal(reconstructed[i], originalData[i]) {
							t.Errorf("Row %d mismatch", i)
						}
					}
				})
			}
		})
	}
}

// TestPropertyRLCRSCommutation tests that RLC and RS extension commute
func TestPropertyRLCRSCommutation(t *testing.T) {
	// This property states that:
	// RLC(RS_extend(data)) = RS_extend(RLC(data))
	// In other words, computing RLC of extended rows should equal extending RLC of original rows

	configs := []struct {
		name string
		k    int
		n    int
	}{
		{"small", 4, 4},
		{"medium", 8, 8},
		{"large", 16, 16},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{
				K:           tc.k,
				N:           tc.n,
				RowSize:     64,
				WorkerCount: 1,
			}

			// Create test data
			data := makeTestData(tc.k, 64)

			// Encode to get extended data
			extData, _, err := Encode(data, config)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			// Get coefficients
			coeffs := deriveCoefficients(extData.rowRoot, config)

			// For each extended row, verify RLC commutation
			for i := tc.k; i < tc.k+tc.n; i++ {
				// Method 1: Compute RLC of the extended row directly
				directRLC := computeRLC(extData.rows[i], coeffs, config)

				// Method 2: Use RS-extended RLC values
				// (This is already computed in extData during encoding)
				// We need to recompute it to verify
				extendedRLCs, err := encoding.ExtendRLCResults(extData.rlcOrig, tc.n)
				if err != nil {
					t.Fatalf("ExtendRLCResults failed: %v", err)
				}

				if !field.Equal128(directRLC, extendedRLCs[i]) {
					t.Errorf("RLC-RS commutation failed for row %d", i)
				}
			}
		})
	}
}

// TestPropertyProofCompleteness tests that every row can be proven and verified
func TestPropertyProofCompleteness(t *testing.T) {
	config := &Config{
		K:           8,
		N:           24, // 1:3 ratio
		RowSize:     128,
		WorkerCount: 4, // Test with parallelism
	}

	data := makeTestData(config.K, config.RowSize)
	extData, commitment, err := Encode(data, config)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Every row should have a valid proof
	for i := 0; i < config.K+config.N; i++ {
		proof, err := extData.GenerateProof(i)
		if err != nil {
			t.Errorf("Failed to generate proof for row %d: %v", i, err)
			continue
		}

		if err := VerifyProof(proof, commitment, config); err != nil {
			t.Errorf("Failed to verify proof for row %d: %v", i, err)
		}
	}
}

// TestPropertyEncodingInvertibility tests that K rows are sufficient for reconstruction
func TestPropertyEncodingInvertibility(t *testing.T) {
	config := &Config{
		K:           16,
		N:           16,
		RowSize:     256,
		WorkerCount: 1,
	}

	// Create test data with unique content
	originalData := make([][]byte, config.K)
	for i := 0; i < config.K; i++ {
		originalData[i] = make([]byte, config.RowSize)
		// Fill with unique pattern
		for j := 0; j < config.RowSize; j++ {
			originalData[i][j] = byte((i ^ j) % 256)
		}
	}

	extData, _, err := Encode(originalData, config)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Test that any K rows are sufficient
	// We'll test a few different selections
	selections := [][]int{
		// First K rows
		makeRange(0, config.K),
		// Last K rows
		makeRange(config.N, config.K+config.N)[:config.K],
		// Every other row
		func() []int {
			indices := []int{}
			for i := 0; i < config.K+config.N && len(indices) < config.K; i += 2 {
				indices = append(indices, i)
			}
			return indices
		}(),
	}

	for idx, indices := range selections {
		t.Run(fmt.Sprintf("selection_%d", idx), func(t *testing.T) {
			rows := make([][]byte, config.K)
			for i, rowIdx := range indices {
				rows[i] = extData.rows[rowIdx]
			}

			reconstructed, err := Reconstruct(rows, indices, config)
			if err != nil {
				t.Fatalf("Reconstruction failed: %v", err)
			}

			// Verify exact match
			for i := 0; i < config.K; i++ {
				if !bytes.Equal(reconstructed[i], originalData[i]) {
					// Find first difference for debugging
					for j := 0; j < len(originalData[i]); j++ {
						if reconstructed[i][j] != originalData[i][j] {
							t.Errorf("Row %d differs at byte %d: got %02x, want %02x",
								i, j, reconstructed[i][j], originalData[i][j])
							break
						}
					}
				}
			}
		})
	}
}

// Helper function for creating alternating indices
func makeAlternatingIndices(k, n int) []int {
	indices := make([]int, k)
	for i := 0; i < k; i++ {
		if i%2 == 0 {
			indices[i] = i / 2 // Original rows
		} else {
			indices[i] = k + i/2 // Parity rows
		}
	}
	return indices
}