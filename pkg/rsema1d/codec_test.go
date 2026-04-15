package rsema1d

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/encoding"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/stretchr/testify/require"
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
	// Power of 2 cases (original)
	{name: "1:1 small k=4 n=4", k: 4, n: 4, rowSize: 64},
	{name: "1:3 small k=4 n=12", k: 4, n: 12, rowSize: 64},
	{name: "1:1 medium k=8 n=8", k: 8, n: 8, rowSize: 256},
	{name: "1:3 medium k=8 n=24", k: 8, n: 24, rowSize: 256},
	{name: "1:1 large k=16 n=16", k: 16, n: 16, rowSize: 512},
	{name: "1:3 large k=16 n=48", k: 16, n: 48, rowSize: 512},

	// Arbitrary K and N cases
	{name: "arbitrary k=3 n=5", k: 3, n: 5, rowSize: 64},
	{name: "arbitrary k=5 n=7", k: 5, n: 7, rowSize: 128},
	{name: "arbitrary k=7 n=9", k: 7, n: 9, rowSize: 128},
	{name: "arbitrary k=10 n=15", k: 10, n: 15, rowSize: 256},
	{name: "arbitrary k=13 n=19", k: 13, n: 19, rowSize: 256},
	{name: "arbitrary k=17 n=31", k: 17, n: 31, rowSize: 512},
	{name: "arbitrary k=100 n=150", k: 100, n: 150, rowSize: 512},
	{name: "arbitrary k=127 n=129", k: 127, n: 129, rowSize: 512},
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
		0,         // First original row
		k - 1,     // Last original row
		k,         // First extended row
		k + n - 1, // Last extended row
		k / 2,     // Middle original row
		k + n/2,   // Middle extended row
	}
}

// Main tests

// TestVerifyAllRowsWithCachedContext verifies that all K+N rows in an encoding
// can be verified against a single VerificationContext. This exercises the
// coefficient caching path: deriveCoefficients runs once on the first call
// and the cached result is reused for all subsequent rows.
func TestVerifyAllRowsWithCachedContext(t *testing.T) {
	config := &Config{
		K:           8,
		N:           8,
		RowSize:     256,
		WorkerCount: 1,
	}

	data := makeTestData(config.K, config.RowSize)
	extData, commitment, _, err := Encode(data, config)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	ctx, _, err := CreateVerificationContext(extData.rlcOrig, config)
	if err != nil {
		t.Fatalf("CreateVerificationContext() error: %v", err)
	}

	// Verify every row (both original and parity) with the same context
	for i := range config.K + config.N {
		proof, err := extData.GenerateRowProof(i)
		if err != nil {
			t.Fatalf("GenerateRowProof(%d) error: %v", i, err)
		}
		if err := VerifyRowWithContext(proof, commitment, ctx); err != nil {
			t.Errorf("VerifyRowWithContext(%d) error: %v", i, err)
		}
	}
}

func TestEncodeAndVerifyWithContext(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)

			if err := config.Validate(); err != nil {
				t.Fatalf("Config validation failed: %v", err)
			}

			data := makeTestData(tc.k, tc.rowSize)
			extData, commitment, _, err := Encode(data, config)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			// Create verification context
			ctx, _, err := CreateVerificationContext(extData.rlcOrig, config)
			if err != nil {
				t.Fatalf("CreateVerificationContext() error: %v", err)
			}

			for _, index := range getTestRowIndices(tc.k, tc.n) {
				t.Run(fmt.Sprintf("row_%d", index), func(t *testing.T) {
					testRowProofGeneration(t, extData, commitment, config, ctx, index)
				})
			}
		})
	}
}

func testRowProofGeneration(t *testing.T, extData *ExtendedData, commitment Commitment, config *Config, ctx *VerificationContext, index int) {
	proof, err := extData.GenerateRowProof(index)
	if err != nil {
		t.Fatalf("GenerateRowProof(%d) error: %v", index, err)
	}

	// Verify proof with context
	if err := VerifyRowWithContext(proof, commitment, ctx); err != nil {
		t.Errorf("VerifyRowWithContext(%d) error: %v", index, err)
	}

	// Verify proof contains correct row data
	if !bytes.Equal(proof.Row, extData.rows[index]) {
		t.Errorf("Row proof data doesn't match extended data for index %d", index)
	}

	// For original rows, also test standalone proof
	if index < config.K {
		testStandaloneProof(t, extData, commitment, config, index)
	}
}

func testStandaloneProof(t *testing.T, extData *ExtendedData, commitment Commitment, config *Config, index int) {
	proof, err := extData.GenerateStandaloneProof(index)
	if err != nil {
		t.Fatalf("GenerateStandaloneProof(%d) error: %v", index, err)
	}

	// Verify standalone proof
	if err := VerifyStandaloneProof(proof, commitment, config); err != nil {
		t.Errorf("VerifyStandaloneProof(%d) error: %v", index, err)
	}

	// Verify proof contains correct row data
	if !bytes.Equal(proof.Row, extData.rows[index]) {
		t.Errorf("Standalone proof data doesn't match extended data for index %d", index)
	}
}

func TestReconstruction(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			originalData := makeTestData(tc.k, tc.rowSize)

			extData, _, _, err := Encode(originalData, config)
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

			extData, _, _, err := Encode(data, config)
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
				rowRLC := computeRLC(extData.rows[i], coeffs)
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

			extData, commitment, _, err := Encode(data, config)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			// Create verification context
			ctx, _, err := CreateVerificationContext(extData.rlcOrig, config)
			if err != nil {
				t.Fatalf("CreateVerificationContext() error: %v", err)
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
					// Test context-based verification
					t.Run("context_verification", func(t *testing.T) {
						testInvalidProofVariations(t, extData, commitment, config, ctx, index)
					})

					// Test standalone verification (only for original rows)
					if index < tc.k {
						t.Run("standalone_verification", func(t *testing.T) {
							testInvalidStandaloneProofVariations(t, extData, commitment, config, index)
						})
					}
				})
			}
		})
	}
}

// getCommonRowProofTests returns test cases that apply to both RowProof and StandaloneProof
func getCommonRowProofTests(index int, config *Config) []struct {
	name       string
	corruptRow func(*RowProof) *RowProof
} {
	return []struct {
		name       string
		corruptRow func(*RowProof) *RowProof
	}{
		{
			name:       "valid_proof",
			corruptRow: func(p *RowProof) *RowProof { return p },
		},
		{
			name: "wrong_index",
			corruptRow: func(p *RowProof) *RowProof {
				modified := *p
				// For context proofs, can be any valid index
				// For standalone, must stay within original rows
				if index < config.K {
					modified.Index = (index + 1) % config.K
				} else {
					modified.Index = (index + 1) % (config.K + config.N)
				}
				return &modified
			},
		},
		{
			name: "corrupted_row_data",
			corruptRow: func(p *RowProof) *RowProof {
				modified := *p
				modified.Row = make([]byte, len(p.Row))
				copy(modified.Row, p.Row)
				modified.Row[0] ^= 0xFF
				return &modified
			},
		},
		{
			name: "corrupted_row_proof",
			corruptRow: func(p *RowProof) *RowProof {
				modified := *p
				if len(p.RowProof) > 0 {
					modified.RowProof = copyAndCorruptProofSlice(p.RowProof)
				}
				return &modified
			},
		},
		{
			name: "nil_row_data",
			corruptRow: func(p *RowProof) *RowProof {
				modified := *p
				modified.Row = nil
				return &modified
			},
		},
	}
}

func testInvalidProofVariations(t *testing.T, extData *ExtendedData, commitment Commitment, config *Config, ctx *VerificationContext, index int) {
	validProof, err := extData.GenerateRowProof(index)
	if err != nil {
		t.Fatalf("GenerateRowProof() error: %v", err)
	}

	// Use common test cases
	commonTests := getCommonRowProofTests(index, config)

	for _, tt := range commonTests {
		t.Run(tt.name, func(t *testing.T) {
			modifiedProof := tt.corruptRow(validProof)
			err := VerifyRowWithContext(modifiedProof, commitment, ctx)

			expectError := tt.name != "valid_proof"
			if expectError && err == nil {
				t.Errorf("Expected error but got none")
			} else if !expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func testInvalidStandaloneProofVariations(t *testing.T, extData *ExtendedData, commitment Commitment, config *Config, index int) {
	validProof, err := extData.GenerateStandaloneProof(index)
	if err != nil {
		t.Fatalf("GenerateStandaloneProof() error: %v", err)
	}

	// Get common test cases and apply them to the embedded RowProof
	commonTests := getCommonRowProofTests(index, config)

	// Run common tests by applying corruption to the embedded RowProof
	for _, tt := range commonTests {
		t.Run(tt.name, func(t *testing.T) {
			modified := *validProof
			// Apply the corruption to the embedded RowProof
			corruptedRowProof := tt.corruptRow(&validProof.RowProof)
			modified.RowProof = *corruptedRowProof

			err := VerifyStandaloneProof(&modified, commitment, config)

			expectError := tt.name != "valid_proof"
			if expectError && err == nil {
				t.Errorf("Expected error but got none")
			} else if !expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}

	// Additional tests specific to standalone proofs
	standaloneSpecificTests := []struct {
		name    string
		corrupt func(*StandaloneProof) *StandaloneProof
	}{
		{
			name: "corrupted_rlc_proof",
			corrupt: func(p *StandaloneProof) *StandaloneProof {
				modified := *p
				if len(p.RLCProof) > 0 {
					modified.RLCProof = copyAndCorruptProofSlice(p.RLCProof)
				}
				return &modified
			},
		},
		{
			name: "nil_rlc_proof",
			corrupt: func(p *StandaloneProof) *StandaloneProof {
				modified := *p
				modified.RLCProof = nil
				return &modified
			},
		},
		{
			name: "empty_rlc_proof",
			corrupt: func(p *StandaloneProof) *StandaloneProof {
				modified := *p
				modified.RLCProof = [][]byte{}
				return &modified
			},
		},
	}

	for _, tt := range standaloneSpecificTests {
		t.Run(tt.name, func(t *testing.T) {
			modifiedProof := tt.corrupt(validProof)
			err := VerifyStandaloneProof(modifiedProof, commitment, config)

			// All these should cause errors
			if err == nil {
				t.Errorf("Expected error for %s but got none", tt.name)
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

func TestCorruptedContext(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			data := makeTestData(tc.k, tc.rowSize)

			extData, commitment, _, err := Encode(data, config)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			// Create context with corrupted RLC values
			corruptedRLC := make([]field.GF128, len(extData.rlcOrig))
			copy(corruptedRLC, extData.rlcOrig)
			corruptedRLC[0][0] ^= 1

			badCtx, _, err := CreateVerificationContext(corruptedRLC, config)
			if err != nil {
				t.Fatalf("CreateVerificationContext() error: %v", err)
			}

			// Generate a valid proof
			proof, err := extData.GenerateRowProof(0)
			if err != nil {
				t.Fatalf("GenerateRowProof() error: %v", err)
			}

			// Verification should fail with corrupted context
			if err := VerifyRowWithContext(proof, commitment, badCtx); err == nil {
				t.Errorf("VerifyRowWithContext() should fail with corrupted context")
			}
		})
	}
}

func TestRowInclusionProof(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			data := makeTestData(tc.k, tc.rowSize)
			extData, commitment, rlcOrig, err := Encode(data, config)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			_, rlcOrigRoot, err := CreateVerificationContext(rlcOrig, config)
			if err != nil {
				t.Fatalf("CreateVerificationContext() error: %v", err)
			}

			// Test all rows (original and parity)
			for index := range tc.k + tc.n {
				// Test via ExtendedData
				proof, err := extData.GenerateRowInclusionProof(index)
				if err != nil {
					t.Errorf("GenerateRowInclusionProof(%d) error: %v", index, err)
					continue
				}

				if err := VerifyRowInclusionProof(proof, commitment, config); err != nil {
					t.Errorf("VerifyRowInclusionProof(%d) error: %v", index, err)
				}

				if !bytes.Equal(proof.Row, extData.rows[index]) {
					t.Errorf("Row data mismatch at index %d", index)
				}

				// Test via VerificationContext - construct RowInclusionProof manually
				rowProof, err := extData.GenerateRowProof(index)
				if err != nil {
					t.Errorf("GenerateRowProof(%d) error: %v", index, err)
					continue
				}

				ctxProof := &RowInclusionProof{
					RowProof: *rowProof,
					RLCRoot:  rlcOrigRoot,
				}
				if err := VerifyRowInclusionProof(ctxProof, commitment, config); err != nil {
					t.Errorf("VerifyRowInclusionProof via context(%d) error: %v", index, err)
				}
			}

			// Test invalid cases
			proof, _ := extData.GenerateRowInclusionProof(0)

			// Corrupt commitment
			badCommit := commitment
			badCommit[0] ^= 0xFF
			if err := VerifyRowInclusionProof(proof, badCommit, config); err == nil {
				t.Error("Should fail with corrupted commitment")
			}

			// Corrupt row
			proof.Row[0] ^= 0xFF
			if err := VerifyRowInclusionProof(proof, commitment, config); err == nil {
				t.Error("Should fail with corrupted row")
			}

			// Out of bounds
			if _, err := extData.GenerateRowInclusionProof(-1); err == nil {
				t.Error("Should fail with negative index")
			}
			if _, err := extData.GenerateRowInclusionProof(tc.k + tc.n); err == nil {
				t.Error("Should fail with index >= K+N")
			}
		})
	}
}

func TestEncodeParity(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			data := makeTestData(tc.k, tc.rowSize)

			// Use Encode to get extended rows and expected commitment
			extData1, commitment1, rlcOrig1, err := Encode(data, config)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			// Use EncodeParity with the same extended rows
			extData2, commitment2, rlcOrig2, err := EncodeParity(extData1.rows, config)
			if err != nil {
				t.Fatalf("EncodeParity() error: %v", err)
			}

			// Verify commitments match
			if commitment1 != commitment2 {
				t.Errorf("Commitments don't match:\nEncode:       %x\nEncodeParity: %x", commitment1, commitment2)
			}

			// Verify RLC orig values match
			if len(rlcOrig1) != len(rlcOrig2) {
				t.Errorf("RLC orig length mismatch: Encode=%d, EncodeParity=%d", len(rlcOrig1), len(rlcOrig2))
			}
			for i := range rlcOrig1 {
				if !field.Equal128(rlcOrig1[i], rlcOrig2[i]) {
					t.Errorf("RLC orig mismatch at index %d", i)
				}
			}

			// Verify all rows match
			if len(extData1.rows) != len(extData2.rows) {
				t.Errorf("Row count mismatch: Encode=%d, EncodeParity=%d", len(extData1.rows), len(extData2.rows))
			}
			for i := range extData1.rows {
				if !bytes.Equal(extData1.rows[i], extData2.rows[i]) {
					t.Errorf("Row %d doesn't match", i)
				}
			}

			// Verify proofs work with both
			ctx, _, err := CreateVerificationContext(rlcOrig2, config)
			if err != nil {
				t.Fatalf("CreateVerificationContext() error: %v", err)
			}

			for _, index := range getTestRowIndices(tc.k, tc.n) {
				proof, err := extData2.GenerateRowProof(index)
				if err != nil {
					t.Errorf("GenerateRowProof(%d) error: %v", index, err)
					continue
				}

				if err := VerifyRowWithContext(proof, commitment2, ctx); err != nil {
					t.Errorf("VerifyRowWithContext(%d) error: %v", index, err)
				}
			}
		})
	}
}

func TestValidateRLCRoot(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			data := makeTestData(tc.k, tc.rowSize)

			extData, commitment, _, err := Encode(data, config)
			require.NoError(t, err)

			rlcOrigRoot := extData.rlcOrigRoot

			// Valid: original row proof
			t.Run("valid_original_row", func(t *testing.T) {
				proof, err := extData.GenerateRowProof(0)
				require.NoError(t, err)
				require.NoError(t, ValidateRLCRoot(rlcOrigRoot, commitment, proof, config))
			})

			// Valid: parity row proof
			t.Run("valid_parity_row", func(t *testing.T) {
				proof, err := extData.GenerateRowProof(tc.k)
				require.NoError(t, err)
				require.NoError(t, ValidateRLCRoot(rlcOrigRoot, commitment, proof, config))
			})

			// Valid: last row
			t.Run("valid_last_row", func(t *testing.T) {
				proof, err := extData.GenerateRowProof(tc.k + tc.n - 1)
				require.NoError(t, err)
				require.NoError(t, ValidateRLCRoot(rlcOrigRoot, commitment, proof, config))
			})

			// Wrong RLC root
			t.Run("wrong_rlc_root", func(t *testing.T) {
				proof, err := extData.GenerateRowProof(0)
				require.NoError(t, err)
				badRoot := rlcOrigRoot
				badRoot[0] ^= 0xFF
				require.Error(t, ValidateRLCRoot(badRoot, commitment, proof, config))
			})

			// Wrong commitment
			t.Run("wrong_commitment", func(t *testing.T) {
				proof, err := extData.GenerateRowProof(0)
				require.NoError(t, err)
				badCommitment := commitment
				badCommitment[0] ^= 0xFF
				require.Error(t, ValidateRLCRoot(rlcOrigRoot, badCommitment, proof, config))
			})

			// Corrupted row data in proof
			t.Run("corrupted_row_data", func(t *testing.T) {
				proof, err := extData.GenerateRowProof(0)
				require.NoError(t, err)
				bad := *proof
				bad.Row = make([]byte, len(proof.Row))
				copy(bad.Row, proof.Row)
				bad.Row[0] ^= 0xFF
				require.Error(t, ValidateRLCRoot(rlcOrigRoot, commitment, &bad, config))
			})

			// Corrupted Merkle proof
			t.Run("corrupted_merkle_proof", func(t *testing.T) {
				proof, err := extData.GenerateRowProof(0)
				require.NoError(t, err)
				bad := *proof
				bad.RowProof = copyAndCorruptProofSlice(proof.RowProof)
				require.Error(t, ValidateRLCRoot(rlcOrigRoot, commitment, &bad, config))
			})

			// Wrong index (proof data doesn't match claimed index)
			t.Run("wrong_index", func(t *testing.T) {
				proof, err := extData.GenerateRowProof(0)
				require.NoError(t, err)
				bad := *proof
				bad.Index = 1
				require.Error(t, ValidateRLCRoot(rlcOrigRoot, commitment, &bad, config))
			})

			// Out of bounds index
			t.Run("index_out_of_bounds", func(t *testing.T) {
				proof, err := extData.GenerateRowProof(0)
				require.NoError(t, err)
				bad := *proof
				bad.Index = tc.k + tc.n
				require.Error(t, ValidateRLCRoot(rlcOrigRoot, commitment, &bad, config))
			})

			// Negative index
			t.Run("negative_index", func(t *testing.T) {
				proof, err := extData.GenerateRowProof(0)
				require.NoError(t, err)
				bad := *proof
				bad.Index = -1
				require.Error(t, ValidateRLCRoot(rlcOrigRoot, commitment, &bad, config))
			})
		})
	}
}
