package rsema1d

import (
	"math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/encoding"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

func TestIncorrectEncodingGenerator(t *testing.T) {
	tests := []struct {
		name       string
		tc         testCase
		modifyFunc func(k, n int) []int
	}{
		{
			name: "modify single parity row",
			tc:   testCase{name: "k=4 n=4", k: 4, n: 4, rowSize: 64},
			modifyFunc: func(k, n int) []int {
				return []int{k}
			},
		},
		{
			name: "modify first and last parity rows",
			tc:   testCase{name: "k=8 n=8", k: 8, n: 8, rowSize: 256},
			modifyFunc: func(k, n int) []int {
				return []int{k, k + n - 1}
			},
		},
		{
			name: "modify multiple parity rows",
			tc:   testCase{name: "k=8 n=8", k: 8, n: 8, rowSize: 256},
			modifyFunc: func(k, n int) []int {
				return []int{k, k + 1, k + n/2}
			},
		},
		{
			name: "modify all parity rows",
			tc:   testCase{name: "k=4 n=4", k: 4, n: 4, rowSize: 64},
			modifyFunc: func(k, n int) []int {
				indices := make([]int, n)
				for i := range n {
					indices[i] = k + i
				}
				return indices
			},
		},
		{
			name: "modify single original row",
			tc:   testCase{name: "k=8 n=8", k: 8, n: 8, rowSize: 256},
			modifyFunc: func(k, n int) []int {
				return []int{0}
			},
		},
		{
			name: "modify mixed original and parity rows",
			tc:   testCase{name: "k=8 n=8", k: 8, n: 8, rowSize: 256},
			modifyFunc: func(k, n int) []int {
				return []int{0, 2, k, k + 3}
			},
		},
		{
			name: "modify all original rows",
			tc:   testCase{name: "k=4 n=4", k: 4, n: 4, rowSize: 64},
			modifyFunc: func(k, n int) []int {
				indices := make([]int, k)
				for i := range k {
					indices[i] = i
				}
				return indices
			},
		},
		{
			name: "arbitrary config",
			tc:   testCase{name: "k=5 n=7", k: 5, n: 7, rowSize: 128},
			modifyFunc: func(k, n int) []int {
				return []int{k + 1, k + 3}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := makeTestConfig(tt.tc)
			data := makeTestData(tt.tc.k, tt.tc.rowSize)
			modIndices := tt.modifyFunc(tt.tc.k, tt.tc.n)

			// Get the original commitment for comparison
			_, origCommitment, _, err := Encode(data, config)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			// Regenerate data since Encode consumed it via extension
			data = makeTestData(tt.tc.k, tt.tc.rowSize)

			rng := rand.New(rand.NewSource(42))
			fake, err := GenerateIncorrectEncoding(data, config, modIndices, rng)
			if err != nil {
				t.Fatalf("GenerateIncorrectEncoding() error: %v", err)
			}

			// Commitment should differ from original
			if fake.Commitment == origCommitment {
				t.Error("fake commitment should differ from original")
			}

			// Create verification context from the recomputed RLC values
			ctx, _, err := CreateVerificationContext(fake.RLCOrig, config)
			if err != nil {
				t.Fatalf("CreateVerificationContext() error: %v", err)
			}

			// Build set of modified indices for quick lookup
			modSet := make(map[int]bool, len(modIndices))
			for _, idx := range modIndices {
				modSet[idx] = true
			}

			// Verify each row: modified rows should fail RLC check, unmodified should pass
			for i := 0; i < tt.tc.k+tt.tc.n; i++ {
				proof, err := fake.ExtendedData.GenerateRowProof(i)
				if err != nil {
					t.Fatalf("GenerateRowProof(%d) error: %v", i, err)
				}

				verifyErr := VerifyRowWithContext(proof, fake.Commitment, ctx)
				if modSet[i] {
					if verifyErr == nil {
						t.Errorf("row %d was modified but VerifyRowWithContext succeeded", i)
					}
				} else {
					if verifyErr != nil {
						t.Errorf("row %d was not modified but VerifyRowWithContext failed: %v", i, verifyErr)
					}
				}
			}

			// RowInclusionProofs should pass for ALL rows (including modified)
			// because the Merkle tree and commitment are internally consistent
			for i := 0; i < tt.tc.k+tt.tc.n; i++ {
				proof, err := fake.ExtendedData.GenerateRowInclusionProof(i)
				if err != nil {
					t.Fatalf("GenerateRowInclusionProof(%d) error: %v", i, err)
				}

				if err := VerifyRowInclusionProof(proof, fake.Commitment, config); err != nil {
					t.Errorf("row %d: VerifyRowInclusionProof should pass but failed: %v", i, err)
				}
			}

			// Verify that modified rows actually changed
			for _, idx := range modIndices {
				orig := fake.OriginalRows[idx]
				current := fake.ExtendedData.rows[idx]
				same := true
				for b := range orig {
					if orig[b] != current[b] {
						same = false
						break
					}
				}
				if same {
					t.Errorf("row %d should have been modified but is unchanged", idx)
				}
			}

			// Verify the RLC commutation property directly for all rows:
			// For unmodified rows, RLC(row, coeffs) == extendedRLC[i]
			// For modified rows, RLC(row, coeffs) != extendedRLC[i]
			coeffs := deriveCoefficients(fake.ExtendedData.rowRoot, config.K, config.N, config.RowSize)
			rlcExtended, err := encoding.ExtendRLCResults(fake.RLCOrig, config.N)
			if err != nil {
				t.Fatalf("ExtendRLCResults() error: %v", err)
			}

			for i := 0; i < tt.tc.k+tt.tc.n; i++ {
				rowRLC := computeRLC(fake.ExtendedData.rows[i], coeffs)
				matches := field.Equal128(rowRLC, rlcExtended[i])
				if modSet[i] {
					if matches {
						t.Errorf("row %d: RLC commutation should fail but matched", i)
					}
				} else {
					if !matches {
						t.Errorf("row %d: RLC commutation should hold but didn't", i)
					}
				}
			}
		})
	}
}
