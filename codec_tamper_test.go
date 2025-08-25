package rsema1d

import (
	"crypto/sha256"
	"testing"

	"github.com/celestiaorg/rsema1d/encoding"
	"github.com/celestiaorg/rsema1d/field"
	"github.com/celestiaorg/rsema1d/merkle"
)

// TestTamperedExtendedDataBeforeCommitment tests that if extended data is tampered with
// after RS encoding but before commitment generation, proofs will fail verification
func TestTamperedExtendedDataBeforeCommitment(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			
			// Create test data
			data := makeTestData(config.K, config.RowSize)

			// Manually perform the encoding steps to allow tampering
			// Step 1: Extend data using RS
			extended, err := encoding.ExtendVertical(data, config.N)
			if err != nil {
				t.Fatalf("ExtendVertical failed: %v", err)
			}

			// TAMPER: Modify one of the extended (parity) rows
			tamperedIndex := config.K + 1 // Second parity row
			extended[tamperedIndex][0] ^= 0xFF

			// Continue with the rest of the encoding process
			// Step 2: Compute row hashes and Merkle tree
			rowHashes := computeRowHashes(extended, config.WorkerCount)
			rowTree := merkle.NewTree(rowHashes)
			rowRoot := rowTree.Root()

			// Step 3: Derive RLC coefficients
			coeffs := deriveCoefficients(rowRoot, config)

			// Step 4: Compute RLC results for original rows
			rlcOrig := computeRLCOrig(data, coeffs, config)

			// Step 5: Extend RLC results
			rlcExtended, err := encoding.ExtendRLCResults(rlcOrig, config.N)
			if err != nil {
				t.Fatalf("ExtendRLCResults failed: %v", err)
			}

			// Step 6: Build RLC Merkle tree
			rlcLeaves := make([][]byte, len(rlcExtended))
			for i, result := range rlcExtended {
				bytes := field.ToBytes128(result)
				rlcLeaves[i] = bytes[:]
			}
			rlcTree := merkle.NewTree(rlcLeaves)
			rlcRoot := rlcTree.Root()

			// Step 7: Create commitment
			h := sha256.New()
			h.Write(rowRoot[:])
			h.Write(rlcRoot[:])
			var commitment Commitment
			h.Sum(commitment[:0])

			// Create ExtendedData structure
			extData := &ExtendedData{
				config:    config,
				rows:      extended,
				rowRoot:   rowRoot,
				rlcRoot:   rlcRoot,
				rowHashes: rowHashes,
				rlcOrig:   rlcOrig,
				rowTree:   rowTree,
				rlcTree:   rlcTree,
			}

			// Generate proof for the tampered row
			proof, err := extData.GenerateProof(tamperedIndex)
			if err != nil {
				t.Fatalf("GenerateProof failed: %v", err)
			}

			// The proof should FAIL verification because:
			// 1. The row data in the proof is tampered
			// 2. When verifier computes RLC of the tampered row, it won't match the extended RLC
			err = VerifyProof(proof, commitment, config)
			if err == nil {
				t.Error("Proof verification should fail for tampered extended row, but it passed")
			} else {
				t.Logf("Expected failure: %v", err)
			}

			// Also test an original row to ensure the system still works for untampered rows
			originalProof, err := extData.GenerateProof(0)
			if err != nil {
				t.Fatalf("GenerateProof for original row failed: %v", err)
			}

			err = VerifyProof(originalProof, commitment, config)
			if err != nil {
				t.Errorf("Proof verification should pass for untampered original row, but failed: %v", err)
			}
		})
	}
}

// TestTamperedRLCBeforeCommitment tests that if RLC results are tampered with
// after computation but before commitment generation, proofs will fail verification
func TestTamperedRLCBeforeCommitment(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)

			// Create test data
			data := makeTestData(config.K, config.RowSize)

			// Manually perform the encoding steps to allow tampering
			// Step 1: Extend data using RS
			extended, err := encoding.ExtendVertical(data, config.N)
			if err != nil {
				t.Fatalf("ExtendVertical failed: %v", err)
			}

			// Step 2: Compute row hashes and Merkle tree
			rowHashes := computeRowHashes(extended, config.WorkerCount)
			rowTree := merkle.NewTree(rowHashes)
			rowRoot := rowTree.Root()

			// Step 3: Derive RLC coefficients
			coeffs := deriveCoefficients(rowRoot, config)

			// Step 4: Compute RLC results for original rows
			rlcOrig := computeRLCOrig(data, coeffs, config)

			// Step 5: Extend RLC results
			rlcExtended, err := encoding.ExtendRLCResults(rlcOrig, config.N)
			if err != nil {
				t.Fatalf("ExtendRLCResults failed: %v", err)
			}

			// TAMPER: Modify one of the extended RLC values
			tamperedRLCIndex := config.K + 2 // Third parity row's RLC
			rlcExtended[tamperedRLCIndex][0] ^= 0xFFFF

			// Step 6: Build RLC Merkle tree with tampered data
			rlcLeaves := make([][]byte, len(rlcExtended))
			for i, result := range rlcExtended {
				bytes := field.ToBytes128(result)
				rlcLeaves[i] = bytes[:]
			}
			rlcTree := merkle.NewTree(rlcLeaves)
			rlcRoot := rlcTree.Root()

			// Step 7: Create commitment with tampered RLC root
			h := sha256.New()
			h.Write(rowRoot[:])
			h.Write(rlcRoot[:])
			var commitment Commitment
			h.Sum(commitment[:0])

			// Create ExtendedData structure
			extData := &ExtendedData{
				config:    config,
				rows:      extended,
				rowRoot:   rowRoot,
				rlcRoot:   rlcRoot,
				rowHashes: rowHashes,
				rlcOrig:   rlcOrig,
				rowTree:   rowTree,
				rlcTree:   rlcTree,
			}

			// Generate proof for the row whose RLC was tampered
			proof, err := extData.GenerateProof(tamperedRLCIndex)
			if err != nil {
				t.Fatalf("GenerateProof failed: %v", err)
			}

			// The proof should FAIL verification because:
			// 1. The verifier will compute the correct RLC from the row data
			// 2. But when it extends the original RLCs, it will get the correct extended value
			// 3. This won't match the tampered value in the commitment
			err = VerifyProof(proof, commitment, config)
			if err == nil {
				t.Error("Proof verification should fail for row with tampered RLC, but it passed")
			} else {
				t.Logf("Expected failure: %v", err)
			}
		})
	}
}

// TestTamperedOriginalRLCBeforeCommitment tests that tampering with original RLC values
// before commitment affects all extended row proofs
func TestTamperedOriginalRLCBeforeCommitment(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)

			// Create test data
			data := makeTestData(config.K, config.RowSize)

			// Manually perform the encoding steps to allow tampering
			// Step 1: Extend data using RS
			extended, err := encoding.ExtendVertical(data, config.N)
			if err != nil {
				t.Fatalf("ExtendVertical failed: %v", err)
			}

			// Step 2: Compute row hashes and Merkle tree
			rowHashes := computeRowHashes(extended, config.WorkerCount)
			rowTree := merkle.NewTree(rowHashes)
			rowRoot := rowTree.Root()

			// Step 3: Derive RLC coefficients
			coeffs := deriveCoefficients(rowRoot, config)

			// Step 4: Compute RLC results for original rows
			rlcOrig := computeRLCOrig(data, coeffs, config)

			// TAMPER: Modify one of the original RLC values
			// Use min to ensure we don't exceed array bounds for small K
			tamperedOrigIndex := min(2, config.K-1)
			rlcOrig[tamperedOrigIndex][0] ^= 0xFFFF

			// Step 5: Extend RLC results (will now produce wrong extended values)
			rlcExtended, err := encoding.ExtendRLCResults(rlcOrig, config.N)
			if err != nil {
				t.Fatalf("ExtendRLCResults failed: %v", err)
			}

			// Step 6: Build RLC Merkle tree
			rlcLeaves := make([][]byte, len(rlcExtended))
			for i, result := range rlcExtended {
				bytes := field.ToBytes128(result)
				rlcLeaves[i] = bytes[:]
			}
			rlcTree := merkle.NewTree(rlcLeaves)
			rlcRoot := rlcTree.Root()

			// Step 7: Create commitment
			h := sha256.New()
			h.Write(rowRoot[:])
			h.Write(rlcRoot[:])
			var commitment Commitment
			h.Sum(commitment[:0])

			// Create ExtendedData structure with tampered rlcOrig
			extData := &ExtendedData{
				config:    config,
				rows:      extended,
				rowRoot:   rowRoot,
				rlcRoot:   rlcRoot,
				rowHashes: rowHashes,
				rlcOrig:   rlcOrig, // This contains the tampered value
				rowTree:   rowTree,
				rlcTree:   rlcTree,
			}

			// Test 1: Original row proof at tampered index should fail
			proof, err := extData.GenerateProof(tamperedOrigIndex)
			if err != nil {
				t.Fatalf("GenerateProof failed: %v", err)
			}

			err = VerifyProof(proof, commitment, config)
			if err == nil {
				t.Error("Proof verification should fail for original row with tampered RLC, but it passed")
			} else {
				t.Logf("Expected failure for original row: %v", err)
			}

			// Test 2: Extended row proofs should also fail because they depend on tampered original RLCs
			extendedProof, err := extData.GenerateProof(config.K + 1)
			if err != nil {
				t.Fatalf("GenerateProof for extended row failed: %v", err)
			}

			err = VerifyProof(extendedProof, commitment, config)
			if err == nil {
				t.Error("Proof verification should fail for extended row when original RLCs are tampered, but it passed")
			} else {
				t.Logf("Expected failure for extended row: %v", err)
			}
		})
	}
}

// TestMultipleTamperedRows tests that tampering with multiple rows is detected
func TestMultipleTamperedRows(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)

			// Create test data
			data := makeTestData(config.K, config.RowSize)

			// Manually perform the encoding steps to allow tampering
			extended, err := encoding.ExtendVertical(data, config.N)
			if err != nil {
				t.Fatalf("ExtendVertical failed: %v", err)
			}

			// TAMPER: Modify multiple extended rows
			// Use safe indices that work for all test cases
			tamperedIndices := []int{config.K} // First extended row always exists
			if config.N > 3 {
				tamperedIndices = append(tamperedIndices, config.K+3)
			}
			if config.N > 5 {
				tamperedIndices = append(tamperedIndices, config.K+5)
			}
			
			for _, idx := range tamperedIndices {
				extended[idx][10%len(extended[idx])] ^= 0xAA
				if len(extended[idx]) > 20 {
					extended[idx][20] ^= 0xBB
				}
			}

			// Continue with encoding
			rowHashes := computeRowHashes(extended, config.WorkerCount)
			rowTree := merkle.NewTree(rowHashes)
			rowRoot := rowTree.Root()

			coeffs := deriveCoefficients(rowRoot, config)
			rlcOrig := computeRLCOrig(data, coeffs, config)
			rlcExtended, err := encoding.ExtendRLCResults(rlcOrig, config.N)
			if err != nil {
				t.Fatalf("ExtendRLCResults failed: %v", err)
			}

			rlcLeaves := make([][]byte, len(rlcExtended))
			for i, result := range rlcExtended {
				bytes := field.ToBytes128(result)
				rlcLeaves[i] = bytes[:]
			}
			rlcTree := merkle.NewTree(rlcLeaves)
			rlcRoot := rlcTree.Root()

			h := sha256.New()
			h.Write(rowRoot[:])
			h.Write(rlcRoot[:])
			var commitment Commitment
			h.Sum(commitment[:0])

			extData := &ExtendedData{
				config:    config,
				rows:      extended,
				rowRoot:   rowRoot,
				rlcRoot:   rlcRoot,
				rowHashes: rowHashes,
				rlcOrig:   rlcOrig,
				rowTree:   rowTree,
				rlcTree:   rlcTree,
			}

			// All tampered rows should fail verification
			for _, idx := range tamperedIndices {
				proof, err := extData.GenerateProof(idx)
				if err != nil {
					t.Fatalf("GenerateProof(%d) failed: %v", idx, err)
				}

				err = VerifyProof(proof, commitment, config)
				if err == nil {
					t.Errorf("Row %d: Proof verification should fail for tampered row, but it passed", idx)
				} else {
					t.Logf("Row %d: Expected failure: %v", idx, err)
				}
			}

			// Untampered rows should still verify correctly
			// Pick a safe untampered index
			untamperedIndex := config.K + min(2, config.N-1)
			isUntampered := true
			for _, idx := range tamperedIndices {
				if idx == untamperedIndex {
					isUntampered = false
					break
				}
			}
			
			if isUntampered {
				proof, err := extData.GenerateProof(untamperedIndex)
				if err != nil {
					t.Fatalf("GenerateProof(%d) failed: %v", untamperedIndex, err)
				}

				err = VerifyProof(proof, commitment, config)
				if err != nil {
					t.Errorf("Row %d: Proof verification should pass for untampered row, but failed: %v", untamperedIndex, err)
				}
			}
		})
	}
}