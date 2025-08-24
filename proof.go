package rsema1d

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/celestiaorg/rsema1d/encoding"
	"github.com/celestiaorg/rsema1d/field"
	"github.com/celestiaorg/rsema1d/merkle"
)

// VerifyProof verifies a row proof against commitment
func VerifyProof(proof *Proof, commitment Commitment, config *Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// 1. Compute rowRoot from the row proof
	rowHash := sha256.Sum256(proof.Row)
	rowRoot, err := computeRootFromProof(rowHash[:], proof.Index, proof.RowProof)
	if err != nil {
		return fmt.Errorf("failed to compute row root: %w", err)
	}

	// 2. Derive RLC coefficients using the computed rowRoot
	coeffs := deriveCoefficients(rowRoot, config)

	// 3. Compute RLC for the proof row
	rowRLC := computeRLC(proof.Row, coeffs, config)

	// 4. Compute rlcRoot based on proof type
	var rlcRoot [32]byte
	if proof.Type(config) == ProofTypeOriginal {
		// For original rows: compute rlcRoot from the RLC proof
		rlcBytes := field.ToBytes128(rowRLC)
		rlcRoot, err = computeRootFromProof(rlcBytes[:], proof.Index, proof.RLCProof)
		if err != nil {
			return fmt.Errorf("failed to compute RLC root: %w", err)
		}
	} else {
		// For extended rows: verify using the original RLCs
		rlcRoot, err = verifyExtendedRow(proof, rowRLC, config)
		if err != nil {
			return err
		}
	}

	// 5. Verify the commitment matches SHA256(rowRoot || rlcRoot)
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcRoot[:])
	computedCommitment := h.Sum(nil)

	if commitment != [32]byte(computedCommitment) {
		return errors.New("commitment verification failed")
	}

	return nil
}

// computeRootFromProof computes the Merkle root given a leaf and its proof
func computeRootFromProof(leaf []byte, index int, proof [][]byte) ([32]byte, error) {
	// Start with the leaf
	current := leaf
	pos := index

	// Traverse up the tree using the proof
	for _, sibling := range proof {
		if pos%2 == 0 {
			// Current is left child
			current = hashPair(current, sibling)
		} else {
			// Current is right child
			current = hashPair(sibling, current)
		}
		pos /= 2
	}

	var root [32]byte
	copy(root[:], current)
	return root, nil
}

// verifyExtendedRow verifies an extended row and returns the computed rlcRoot
func verifyExtendedRow(proof *Proof, computedRLC field.GF128, config *Config) ([32]byte, error) {
	// Extend the original RLC results to get all K+N RLC values
	extendedRLCs, err := encoding.ExtendRLCResults(proof.YOrig, config.K, config.N)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to extend RLC results: %w", err)
	}

	// Verify the computed RLC matches the extended value at proof.Index
	if proof.Index >= len(extendedRLCs) {
		return [32]byte{}, fmt.Errorf("proof index %d out of range", proof.Index)
	}

	expectedRLC := extendedRLCs[proof.Index]
	if !rlcEqual(computedRLC, expectedRLC) {
		return [32]byte{}, errors.New("computed RLC does not match extended value")
	}

	// The YLeftProof proves that the K original RLCs (proof.YOrig) are the first K leaves
	// of a Merkle tree with K+N leaves

	// Convert the K original RLCs to bytes for Merkle tree
	origLeaves := make([][]byte, config.K)
	for i, rlc := range proof.YOrig {
		bytes := field.ToBytes128(rlc)
		origLeaves[i] = bytes[:]
	}
	
	// Compute Merkle root of the K original RLCs
	origRoot := merkle.ComputeSubtreeRoot(origLeaves)

	// YLeftProof contains sibling subtree roots that allow us to compute
	// from origRoot (root of first K leaves) to the full rlcRoot
	// Each element in YLeftProof is the root of a sibling subtree
	current := origRoot[:]
	
	// Process each sibling in the proof
	for _, sibling := range proof.YLeftProof {
		// At each level, our current subtree is on the left,
		// and the sibling is on the right
		current = hashPair(current, sibling)
	}
	
	var rlcRoot [32]byte
	copy(rlcRoot[:], current)

	// Verify by computing the full tree root directly and comparing
	allLeaves := make([][]byte, len(extendedRLCs))
	for i, rlc := range extendedRLCs {
		bytes := field.ToBytes128(rlc)
		allLeaves[i] = bytes[:]
	}
	expectedRoot := merkle.ComputeSubtreeRoot(allLeaves)
	
	if rlcRoot != expectedRoot {
		return [32]byte{}, errors.New("RLC root verification failed")
	}

	return rlcRoot, nil
}

// hashPair computes SHA256(left || right)
func hashPair(left, right []byte) []byte {
	h := sha256.New()
	h.Write(left)
	h.Write(right)
	return h.Sum(nil)
}

// rlcEqual checks if two GF128 values are equal
func rlcEqual(a, b field.GF128) bool {
	for i := 0; i < 8; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}