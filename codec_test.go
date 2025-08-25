package rsema1d

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/rsema1d/encoding"
	"github.com/celestiaorg/rsema1d/field"
)

// testCase represents a common test configuration
type testCase struct {
	name    string
	k       int
	n       int
	rowSize int
}

// Common test configurations used across all tests
var testCases = []testCase{
	{name: "1:1 small k=4 n=4", k: 4, n: 4, rowSize: 64},
	{name: "1:3 small k=4 n=12", k: 4, n: 12, rowSize: 64},
	{name: "1:1 medium k=8 n=8", k: 8, n: 8, rowSize: 256},
	{name: "1:3 medium k=8 n=24", k: 8, n: 24, rowSize: 256},
	{name: "1:1 large k=16 n=16", k: 16, n: 16, rowSize: 512},
	{name: "1:3 large k=16 n=48", k: 16, n: 48, rowSize: 512},
}

// Helper functions

func makeTestConfig(tc testCase) *Config {
	return &Config{
		K:           tc.k,
		N:           tc.n,
		RowSize:     tc.rowSize,
		WorkerCount: 1,
	}
}

func makeTestData(k, rowSize int) [][]byte {
	data := make([][]byte, k)
	for i := range k {
		data[i] = make([]byte, rowSize)
		for j := range rowSize {
			data[i][j] = byte((i + j) % 256)
		}
	}
	return data
}

func makeRange(start, end int) []int {
	indices := make([]int, end-start)
	for i := range indices {
		indices[i] = start + i
	}
	return indices
}

func makeMixedIndices(k, n int) []int {
	indices := make([]int, k)
	step := (k + n) / k
	
	for i := range k {
		indices[i] = (i * step) % (k + n)
	}
	
	// Ensure uniqueness
	seen := make(map[int]bool)
	for i, idx := range indices {
		for seen[idx] {
			idx = (idx + 1) % (k + n)
		}
		indices[i] = idx
		seen[idx] = true
	}
	
	return indices
}

func getTestRowIndices(k, n int) []int {
	return []int{
		0,           // First original row
		k - 1,       // Last original row
		k,           // First extended row
		k + n - 1,   // Last extended row
		k / 2,       // Middle original row
		k + n/2,     // Middle extended row
	}
}

// Main tests

func TestEncodeAndVerifyProof(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			
			if err := config.Validate(); err != nil {
				t.Fatalf("Config validation failed: %v", err)
			}
			
			data := makeTestData(tc.k, tc.rowSize)
			extData, commitment, err := Encode(data, config)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}
			
			for _, index := range getTestRowIndices(tc.k, tc.n) {
				t.Run(fmt.Sprintf("row_%d", index), func(t *testing.T) {
					testProofGeneration(t, extData, commitment, config, index)
				})
			}
		})
	}
}

func testProofGeneration(t *testing.T, extData *ExtendedData, commitment Commitment, config *Config, index int) {
	proof, err := extData.GenerateProof(index)
	if err != nil {
		t.Fatalf("GenerateProof(%d) error: %v", index, err)
	}
	
	// Verify proof
	if err := VerifyProof(proof, commitment, config); err != nil {
		t.Errorf("VerifyProof(%d) error: %v", index, err)
	}
	
	// Verify proof contains correct row data
	if !bytes.Equal(proof.Row, extData.rows[index]) {
		t.Errorf("Proof row data doesn't match extended data for index %d", index)
	}
	
	// Check proof type and structure
	validateProofStructure(t, proof, config, index)
}

func validateProofStructure(t *testing.T, proof *Proof, config *Config, index int) {
	proofType := proof.Type(config)
	
	if index < config.K {
		// Original row checks
		if proofType != ProofTypeOriginal {
			t.Errorf("Expected ProofTypeOriginal for index %d, got %v", index, proofType)
		}
		if proof.RLCProof == nil {
			t.Errorf("Original proof missing RLCProof for index %d", index)
		}
		if proof.RLCOrig != nil {
			t.Errorf("Original proof should not have RLCOrig for index %d", index)
		}
	} else {
		// Extended row checks
		if proofType != ProofTypeExtended {
			t.Errorf("Expected ProofTypeExtended for index %d, got %v", index, proofType)
		}
		if proof.RLCOrig == nil {
			t.Errorf("Extended proof missing RLCOrig for index %d", index)
		}
		if len(proof.RLCOrig) != config.K {
			t.Errorf("Extended proof RLCOrig has %d values, want %d", len(proof.RLCOrig), config.K)
		}
		if proof.RLCProof != nil {
			t.Errorf("Extended proof should not have RLCProof for index %d", index)
		}
		
		// Verify RLC value sizes
		for j, rlc := range proof.RLCOrig {
			if len(rlc) != 16 {
				t.Errorf("RLCOrig[%d] has %d bytes, want 16", j, len(rlc))
			}
		}
	}
}

func TestReconstruction(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			originalData := makeTestData(tc.k, tc.rowSize)
			
			extData, _, err := Encode(originalData, config)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}
			
			reconstructionTests := []struct {
				name    string
				indices []int
			}{
				{"original_rows", makeRange(0, tc.k)},
				{"parity_rows", makeRange(tc.k, tc.k+tc.n)[:tc.k]},
				{"mixed_rows", makeMixedIndices(tc.k, tc.n)},
			}
			
			for _, rt := range reconstructionTests {
				t.Run(rt.name, func(t *testing.T) {
					testReconstructFromIndices(t, extData, originalData, rt.indices, config)
				})
			}
		})
	}
}

func testReconstructFromIndices(t *testing.T, extData *ExtendedData, originalData [][]byte, indices []int, config *Config) {
	rows := make([][]byte, len(indices))
	for i, idx := range indices {
		rows[i] = extData.rows[idx]
	}
	
	reconstructed, err := Reconstruct(rows, indices, config)
	if err != nil {
		t.Fatalf("Reconstruct() error: %v", err)
	}
	
	if len(reconstructed) != config.K {
		t.Errorf("Reconstruct() returned %d rows, want %d", len(reconstructed), config.K)
	}
	
	for i := range config.K {
		if !bytes.Equal(reconstructed[i], originalData[i]) {
			t.Errorf("Reconstructed row %d doesn't match original", i)
		}
	}
}

func TestRLCCommutationProperty(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			data := makeTestData(tc.k, tc.rowSize)
			
			extData, _, err := Encode(data, config)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}
			
			coeffs := deriveCoefficients(extData.rowRoot, config)
			extendedRLCs, err := encoding.ExtendRLCResults(extData.rlcOrig, tc.n)
			if err != nil {
				t.Fatalf("ExtendRLCResults() error: %v", err)
			}
			
			// Verify commutation for each extended row
			for i := tc.k; i < tc.k+tc.n; i++ {
				rowRLC := computeRLC(extData.rows[i], coeffs, config)
				if !field.Equal128(rowRLC, extendedRLCs[i]) {
					t.Errorf("RLC commutation failed for row %d", i)
				}
			}
		})
	}
}

func TestInvalidProofs(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			data := makeTestData(tc.k, tc.rowSize)
			
			extData, commitment, err := Encode(data, config)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}
			
			// Test both original and extended row proofs
			proofIndices := []int{0} // Original row
			if tc.n > 0 {
				proofIndices = append(proofIndices, tc.k) // Extended row
			}
			
			for _, index := range proofIndices {
				proofType := "original"
				if index >= tc.k {
					proofType = "extended"
				}
				
				t.Run(proofType, func(t *testing.T) {
					testInvalidProofVariations(t, extData, commitment, config, index)
				})
			}
		})
	}
}

func testInvalidProofVariations(t *testing.T, extData *ExtendedData, commitment Commitment, config *Config, index int) {
	validProof, err := extData.GenerateProof(index)
	if err != nil {
		t.Fatalf("GenerateProof() error: %v", err)
	}
	
	invalidProofTests := []struct {
		name        string
		corrupt     func(*Proof) *Proof
		skipFor     string // "original" or "extended"
	}{
		{
			name: "valid_proof",
			corrupt: func(p *Proof) *Proof { return p },
		},
		{
			name: "wrong_index",
			corrupt: func(p *Proof) *Proof {
				modified := *p
				modified.Index = (index + 1) % (config.K + config.N)
				return &modified
			},
		},
		{
			name: "corrupted_row_data",
			corrupt: func(p *Proof) *Proof {
				modified := *p
				modified.Row = make([]byte, len(p.Row))
				copy(modified.Row, p.Row)
				modified.Row[0] ^= 0xFF
				return &modified
			},
		},
		{
			name: "corrupted_row_proof",
			corrupt: func(p *Proof) *Proof {
				modified := *p
				if len(p.RowProof) > 0 {
					modified.RowProof = copyAndCorruptProofSlice(p.RowProof)
				}
				return &modified
			},
		},
		{
			name: "corrupted_rlc_proof",
			corrupt: func(p *Proof) *Proof {
				modified := *p
				if len(p.RLCProof) > 0 {
					modified.RLCProof = copyAndCorruptProofSlice(p.RLCProof)
				}
				return &modified
			},
			skipFor: "extended",
		},
		{
			name: "corrupted_rlc_orig",
			corrupt: func(p *Proof) *Proof {
				modified := *p
				if len(p.RLCOrig) > 0 {
					modified.RLCOrig = copyAndCorruptProofSlice(p.RLCOrig)
				}
				return &modified
			},
			skipFor: "original",
		},
		{
			name: "nil_row_data",
			corrupt: func(p *Proof) *Proof {
				modified := *p
				modified.Row = nil
				return &modified
			},
		},
	}
	
	for _, tt := range invalidProofTests {
		// Skip tests that don't apply to this proof type
		if tt.skipFor == "original" && index < config.K {
			continue
		}
		if tt.skipFor == "extended" && index >= config.K {
			continue
		}
		
		t.Run(tt.name, func(t *testing.T) {
			modifiedProof := tt.corrupt(validProof)
			err := VerifyProof(modifiedProof, commitment, config)
			
			expectError := tt.name != "valid_proof"
			if expectError && err == nil {
				t.Errorf("Expected error but got none")
			} else if !expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func copyAndCorruptProofSlice(original [][]byte) [][]byte {
	if len(original) == 0 {
		return original
	}
	
	modified := make([][]byte, len(original))
	for i := range original {
		modified[i] = make([]byte, len(original[i]))
		copy(modified[i], original[i])
	}
	modified[0][0] ^= 0xFF
	return modified
}

func TestExtendedProofTampering(t *testing.T) {
	for _, tc := range testCases {
		if tc.n == 0 {
			continue // Skip if no extended rows
		}
		
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			data := makeTestData(tc.k, tc.rowSize)
			
			extData, commitment, err := Encode(data, config)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}
			
			// Test first extended row
			proof, err := extData.GenerateProof(tc.k)
			if err != nil {
				t.Fatalf("GenerateProof() error: %v", err)
			}
			
			// Tamper with RLCOrig
			tamperedProof := *proof
			tamperedProof.RLCOrig = make([][]byte, len(proof.RLCOrig))
			for i := range proof.RLCOrig {
				tamperedProof.RLCOrig[i] = make([]byte, 16)
				copy(tamperedProof.RLCOrig[i], proof.RLCOrig[i])
			}
			tamperedProof.RLCOrig[0][0] ^= 0xFF
			
			if err := VerifyProof(&tamperedProof, commitment, config); err == nil {
				t.Errorf("VerifyProof() should fail with tampered RLCOrig")
			}
		})
	}
}