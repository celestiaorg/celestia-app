package rsema1d

import (
	"crypto/sha256"
	"crypto/sha512"
	"slices"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/pkg/rsema1d/encoding"
	"github.com/celestiaorg/celestia-app/v8/pkg/rsema1d/merkle"
	"github.com/stretchr/testify/assert"
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
			// Step 2: Build padded Merkle tree (need to validate config first)
			if err := config.Validate(); err != nil {
				t.Fatalf("Config validation failed: %v", err)
			}
			rowTree := buildPaddedRowTree(extended, config)
			rowRoot := rowTree.Root()

			// Step 3: Derive RLC coefficients
			coeffs := deriveCoefficients(rowRoot, config)

			// Step 4: Compute RLC results for original rows
			rlcOrig := computeRLCOrig(data, coeffs, config)

			// Step 5: Build padded RLC Merkle tree
			rlcOrigTree := buildPaddedRLCTree(rlcOrig, config)
			rlcOrigRoot := rlcOrigTree.Root()

			// Step 6: Create commitment
			h := sha256.New()
			h.Write(rowRoot[:])
			h.Write(rlcOrigRoot[:])
			var commitment Commitment
			h.Sum(commitment[:0])

			// Create ExtendedData structure
			extData := &ExtendedData{
				config:      config,
				rows:        extended,
				rowRoot:     rowRoot,
				rlcOrig:     rlcOrig,
				rowTree:     rowTree,
				rlcOrigTree: rlcOrigTree,
				rlcOrigRoot: rlcOrigRoot,
			}

			// Create verification context
			ctx, _, err := CreateVerificationContext(rlcOrig, config)
			if err != nil {
				t.Fatalf("CreateVerificationContext failed: %v", err)
			}

			// Generate proof for the tampered row
			proof, err := extData.GenerateRowProof(tamperedIndex)
			if err != nil {
				t.Fatalf("GenerateRowProof failed: %v", err)
			}

			// The proof should FAIL verification because:
			// 1. The row data in the proof is tampered
			// 2. When verifier computes RLC of the tampered row, it won't match the extended RLC
			err = VerifyRowWithContext(proof, commitment, ctx)
			if err == nil {
				t.Error("Proof verification should fail for tampered extended row, but it passed")
			} else {
				t.Logf("Expected failure: %v", err)
			}

			// Also test an original row to ensure the system still works for untampered rows
			originalProof, err := extData.GenerateRowProof(0)
			if err != nil {
				t.Fatalf("GenerateRowProof for original row failed: %v", err)
			}

			err = VerifyRowWithContext(originalProof, commitment, ctx)
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

			// Step 2: Build padded Merkle tree
			if err := config.Validate(); err != nil {
				t.Fatalf("Config validation failed: %v", err)
			}
			rowTree := buildPaddedRowTree(extended, config)
			rowRoot := rowTree.Root()

			// Step 3: Derive RLC coefficients
			coeffs := deriveCoefficients(rowRoot, config)

			// Step 4: Compute RLC results for original rows
			rlcOrig := computeRLCOrig(data, coeffs, config)

			// TAMPER: Modify one of the extended RLC values
			tamperedRLCIndex := config.K - 1
			rlcOrig[tamperedRLCIndex][0] ^= 0xFFFF

			// Step 5: Build padded RLC Merkle tree
			rlcOrigTree := buildPaddedRLCTree(rlcOrig, config)
			rlcOrigRoot := rlcOrigTree.Root()

			// Step 6: Create commitment with tampered RLC root
			h := sha256.New()
			h.Write(rowRoot[:])
			h.Write(rlcOrigRoot[:])
			var commitment Commitment
			h.Sum(commitment[:0])

			// Create ExtendedData structure
			extData := &ExtendedData{
				config:      config,
				rows:        extended,
				rowRoot:     rowRoot,
				rlcOrig:     rlcOrig,
				rowTree:     rowTree,
				rlcOrigTree: rlcOrigTree,
				rlcOrigRoot: rlcOrigRoot,
			}

			// Create verification context
			ctx, _, err := CreateVerificationContext(rlcOrig, config)
			if err != nil {
				t.Fatalf("CreateVerificationContext failed: %v", err)
			}

			// Generate proof for the row whose RLC was tampered
			proof, err := extData.GenerateRowProof(tamperedRLCIndex)
			if err != nil {
				t.Fatalf("GenerateRowProof failed: %v", err)
			}

			// The proof should FAIL verification because:
			// 1. The verifier will compute the correct RLC from the row data
			// 2. But when it extends the original RLCs, it will get the correct extended value
			// 3. This won't match the tampered value in the commitment
			err = VerifyRowWithContext(proof, commitment, ctx)
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

			// Step 2: Build padded Merkle tree
			if err := config.Validate(); err != nil {
				t.Fatalf("Config validation failed: %v", err)
			}
			rowTree := buildPaddedRowTree(extended, config)
			rowRoot := rowTree.Root()

			// Step 3: Derive RLC coefficients
			coeffs := deriveCoefficients(rowRoot, config)

			// Step 4: Compute RLC results for original rows
			rlcOrig := computeRLCOrig(data, coeffs, config)

			// TAMPER: Modify one of the original RLC values
			// Use min to ensure we don't exceed array bounds for small K
			tamperedOrigIndex := min(2, config.K-1)
			rlcOrig[tamperedOrigIndex][0] ^= 0xFFFF

			// Step 5: Build padded RLC Merkle tree
			rlcOrigTree := buildPaddedRLCTree(rlcOrig, config)
			rlcOrigRoot := rlcOrigTree.Root()

			// Step 6: Create commitment
			h := sha256.New()
			h.Write(rowRoot[:])
			h.Write(rlcOrigRoot[:])
			var commitment Commitment
			h.Sum(commitment[:0])

			// Create ExtendedData structure with tampered rlcOrig
			extData := &ExtendedData{
				config:      config,
				rows:        extended,
				rowRoot:     rowRoot,
				rlcOrig:     rlcOrig, // This contains the tampered value
				rowTree:     rowTree,
				rlcOrigTree: rlcOrigTree,
				rlcOrigRoot: rlcOrigRoot,
			}

			// Create verification context with tampered RLC values
			ctx, _, err := CreateVerificationContext(rlcOrig, config)
			if err != nil {
				t.Fatalf("CreateVerificationContext failed: %v", err)
			}

			// Test 1: Original row proof at tampered index should fail
			proof, err := extData.GenerateRowProof(tamperedOrigIndex)
			if err != nil {
				t.Fatalf("GenerateRowProof failed: %v", err)
			}

			err = VerifyRowWithContext(proof, commitment, ctx)
			if err == nil {
				t.Error("Proof verification should fail for original row with tampered RLC, but it passed")
			} else {
				t.Logf("Expected failure for original row: %v", err)
			}

			// Test 2: Extended row proofs should also fail because they depend on tampered original RLCs
			extendedProof, err := extData.GenerateRowProof(config.K + 1)
			if err != nil {
				t.Fatalf("GenerateRowProof for extended row failed: %v", err)
			}

			err = VerifyRowWithContext(extendedProof, commitment, ctx)
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

			// Continue with encoding - need config validation for padding
			if err := config.Validate(); err != nil {
				t.Fatalf("Config validation failed: %v", err)
			}
			rowTree := buildPaddedRowTree(extended, config)
			rowRoot := rowTree.Root()

			coeffs := deriveCoefficients(rowRoot, config)
			rlcOrig := computeRLCOrig(data, coeffs, config)

			// Build padded RLC Merkle tree
			rlcOrigTree := buildPaddedRLCTree(rlcOrig, config)
			rlcOrigRoot := rlcOrigTree.Root()

			// Create commitment
			h := sha256.New()
			h.Write(rowRoot[:])
			h.Write(rlcOrigRoot[:])
			var commitment Commitment
			h.Sum(commitment[:0])

			extData := &ExtendedData{
				config:      config,
				rows:        extended,
				rowRoot:     rowRoot,
				rlcOrig:     rlcOrig,
				rowTree:     rowTree,
				rlcOrigTree: rlcOrigTree,
				rlcOrigRoot: rlcOrigRoot,
			}

			// Create verification context
			ctx, _, err := CreateVerificationContext(rlcOrig, config)
			if err != nil {
				t.Fatalf("CreateVerificationContext failed: %v", err)
			}

			// All tampered rows should fail verification
			for _, idx := range tamperedIndices {
				proof, err := extData.GenerateRowProof(idx)
				if err != nil {
					t.Fatalf("GenerateRowProof(%d) failed: %v", idx, err)
				}

				err = VerifyRowWithContext(proof, commitment, ctx)
				if err == nil {
					t.Errorf("Row %d: Proof verification should fail for tampered row, but it passed", idx)
				} else {
					t.Logf("Row %d: Expected failure: %v", idx, err)
				}
			}

			// Untampered rows should still verify correctly
			// Pick a safe untampered index
			untamperedIndex := config.K + min(2, config.N-1)
			isUntampered := !slices.Contains(tamperedIndices, untamperedIndex)

			if isUntampered {
				proof, err := extData.GenerateRowProof(untamperedIndex)
				if err != nil {
					t.Fatalf("GenerateRowProof(%d) failed: %v", untamperedIndex, err)
				}

				err = VerifyRowWithContext(proof, commitment, ctx)
				if err != nil {
					t.Errorf("Row %d: Proof verification should pass for untampered row, but failed: %v", untamperedIndex, err)
				}
			}
		})
	}
}

// TestInvalidRowProofDepth tests that tampering with the proof depth is detected
func TestInvalidRowProofDepth(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)

			// Create test data
			data := makeTestData(config.K, config.RowSize)

			// Perform normal encoding
			extended, err := encoding.ExtendVertical(data, config.N)
			if err != nil {
				t.Fatalf("ExtendVertical failed: %v", err)
			}

			if err := config.Validate(); err != nil {
				t.Fatalf("Config validation failed: %v", err)
			}
			rowTree := buildPaddedRowTree(extended, config)
			rowRoot := rowTree.Root()

			coeffs := deriveCoefficients(rowRoot, config)
			rlcOrig := computeRLCOrig(data, coeffs, config)
			if err != nil {
				t.Fatalf("ExtendRLCResults failed: %v", err)
			}

			rlcOrigTree := buildPaddedRLCTree(rlcOrig, config)
			rlcOrigRoot := rlcOrigTree.Root()

			extData := &ExtendedData{
				config:      config,
				rows:        extended,
				rowRoot:     rowRoot,
				rlcOrigRoot: rlcOrigRoot,
				rlcOrig:     rlcOrig,
				rowTree:     rowTree,
				rlcOrigTree: rlcOrigTree,
			}

			// Create verification context
			ctx, rlcOrigRoot, err := CreateVerificationContext(rlcOrig, config)
			if err != nil {
				t.Fatalf("CreateVerificationContext failed: %v", err)
			}

			leafIndex := 1

			validProof, err := extData.GenerateRowProof(leafIndex)
			if err != nil {
				t.Fatalf("GenerateRowProof(%d) failed: %v", leafIndex, err)
			}

			// Create malicious proof with incorrect depth
			// Try to use a higher-level node (depth n-1) at the same tree index
			// This simulates an attacker trying to provide an internal node instead of a leaf
			maliciousProof := &RowProof{
				Index:    leafIndex,
				Row:      extended[leafIndex],
				RowProof: validProof.RowProof[:len(validProof.RowProof)-1], // Remove one level to simulate wrong depth
			}

			treeIndex := mapIndexToTreePosition(maliciousProof.Index, config)
			fakeRowRoot, err := merkle.ComputeRootFromProof(maliciousProof.Row, treeIndex, maliciousProof.RowProof)
			if err != nil {
				t.Errorf("Failed to compute fake row root: %v", err)
			}
			fakeCoeffs := deriveCoefficients(fakeRowRoot, config)
			fakeRlcCommitment := computeRLC(maliciousProof.Row, fakeCoeffs)
			ctx.rlcOrigRoot = rlcOrigRoot

			ctx.rlcExtended[leafIndex] = fakeRlcCommitment

			h := sha256.New()
			h.Write(fakeRowRoot[:])
			h.Write(ctx.rlcOrigRoot[:])
			fakeCommitment := h.Sum(nil)
			err = VerifyRowWithContext(maliciousProof, Commitment(fakeCommitment), ctx)
			assert.Error(t, err, "Expected verification to fail with row size mismatch")
			assert.Contains(t, err.Error(), "row proof depth mismatch")
		})
	}
}

func TestVerifyRowWithContextWithMultipleOpenings(t *testing.T) {
	config := &Config{
		K:           8,
		N:           8,
		RowSize:     256,
		WorkerCount: 1,
	}
	_ = config.Validate() // populate .kPadded and .totalPadded
	// === PROVER ===
	// the prover are being malicious, and they hope that no one will open at index 0
	data := make([][]byte, 8)
	for i := range 8 {
		digest := sha512.Sum512([]byte{byte(i)})
		data[i] = append(digest[:], 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	}
	// mostly copied from the Encode function from codec.go
	extended, err := encoding.ExtendVertical(data, config.N)
	assert.NoError(t, err)
	nodes, asNodes := buildAdversarialPaddedRowTree(extended)
	rowRoot := nodes[0]
	coeffs := deriveCoefficients([32]byte(rowRoot), config)
	rlcOrig := computeRLCOrig(data, coeffs, config)
	rlcOrigTree := buildPaddedRLCTree(rlcOrig, config)
	rlcOrigRoot := rlcOrigTree.Root()
	h := sha256.New()
	h.Write(rowRoot)
	h.Write(rlcOrigRoot[:])
	var commitment Commitment
	h.Sum(commitment[:0])
	// === VERIFIER ===
	// assuming that the verifier wants to open at index 3
	ctx, rlcOrigRoot, err := CreateVerificationContext(rlcOrig, config)
	assert.NoError(t, err)
	// ...it is possible to open as some data (doing nicely)
	proof1 := &RowProof{
		Index:    3,
		Row:      extended[3],
		RowProof: [][]byte{nodes[17], nodes[7], nodes[4], nodes[2]},
	}
	if err := VerifyRowWithContext(proof1, commitment, ctx); err !=
		nil {
		t.Error("VerifyStandaloneProof error:", err)
	}
	// ...attempting to open with truncated data should fail with row size mismatch
	proof2 := &RowProof{
		Index:    3,
		Row:      extended[3][:256-64],
		RowProof: [][]byte{asNodes[17], asNodes[7], asNodes[4], asNodes[2], nodes[16], nodes[8], nodes[4], nodes[2]},
	}
	err = VerifyRowWithContext(proof2, commitment, ctx)
	assert.Error(t, err, "Expected verification to fail with row size mismatch")
	assert.Contains(t, err.Error(), "row size mismatch")
}

func buildAdversarialPaddedRowTree(extended [][]byte) ([][]byte, [][]byte) {
	nodes := make([][]byte, 31)
	asNodes := make([][]byte, 31) // adversary subtree nodes
	// build the adversary subtree
	for i := range 16 {
		digest := sha256.Sum256(append([]byte{0}, extended[i][:256-
			64]...))
		asNodes[15+i] = digest[:] // SHA256(00 || data) for leaf nodes
	}
	for i := range 8 {
		digest := sha256.Sum256(append([]byte{1},
			append(asNodes[15+2*i], asNodes[15+2*i+1]...)...))
		asNodes[7+i] = digest[:] // SHA256(01 || left || right) for non-leaf nodes 20 / 28
	}
	for i := range 4 {
		digest := sha256.Sum256(append([]byte{1},
			append(asNodes[7+2*i], asNodes[7+2*i+1]...)...))
		asNodes[3+i] = digest[:]
	}
	for i := range 2 {
		digest := sha256.Sum256(append([]byte{1},
			append(asNodes[3+2*i], asNodes[3+2*i+1]...)...))
		asNodes[1+i] = digest[:]
	}
	for i := range 1 {
		digest := sha256.Sum256(append([]byte{1},
			append(asNodes[1+2*i], asNodes[1+2*i+1]...)...))
		asNodes[0+i] = digest[:]
	}
	nodes[15+0] = asNodes[0]
	for i := 1; i < 16; i++ { // starts from one
		digest := sha256.Sum256(append([]byte{0}, extended[i][:256]...))
		nodes[15+i] = digest[:]
	}
	for i := range 8 {
		digest := sha256.Sum256(append([]byte{1},
			append(nodes[15+2*i], nodes[15+2*i+1]...)...))
		nodes[7+i] = digest[:]
	}
	for i := range 4 {
		digest := sha256.Sum256(append([]byte{1},
			append(nodes[7+2*i], nodes[7+2*i+1]...)...))
		nodes[3+i] = digest[:]
	}
	for i := range 2 {
		digest := sha256.Sum256(append([]byte{1},
			append(nodes[3+2*i], nodes[3+2*i+1]...)...))
		nodes[1+i] = digest[:]
	}
	for i := range 1 {
		digest := sha256.Sum256(append([]byte{1},
			append(nodes[1+2*i], nodes[1+2*i+1]...)...))
		nodes[0+i] = digest[:]
	}
	return nodes, asNodes
}
