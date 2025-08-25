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
	rowRoot, err := merkle.ComputeRootFromProof(rowHash[:], proof.Index, proof.RowProof)
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
		rlcRoot, err = merkle.ComputeRootFromProof(rlcBytes[:], proof.Index, proof.RLCProof)
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

// verifyExtendedRow verifies an extended row and returns the computed rlcRoot
func verifyExtendedRow(proof *Proof, computedRLC field.GF128, config *Config) ([32]byte, error) {
	// Deserialize RLC results from wire format
	rlcOrig := make([]field.GF128, len(proof.RLCOrig))
	for i, bytes := range proof.RLCOrig {
		if len(bytes) != 16 {
			return [32]byte{}, fmt.Errorf("invalid RLC size at index %d: expected 16 bytes, got %d", i, len(bytes))
		}
		var b16 [16]byte
		copy(b16[:], bytes)
		rlcOrig[i] = field.FromBytes128(b16)
	}

	// Extend the original RLC results to get all K+N RLC values
	extendedRLCs, err := encoding.ExtendRLCResults(rlcOrig, config.N)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to extend RLC results: %w", err)
	}

	// Verify the computed RLC matches the extended value at proof.Index
	if proof.Index >= len(extendedRLCs) {
		return [32]byte{}, fmt.Errorf("proof index %d out of range", proof.Index)
	}

	expectedRLC := extendedRLCs[proof.Index]
	if !field.Equal128(computedRLC, expectedRLC) {
		return [32]byte{}, errors.New("computed RLC does not match extended value")
	}

	// Build the complete RLC Merkle tree from all K+N extended values
	rlcLeaves := make([][]byte, len(extendedRLCs))
	for i, rlc := range extendedRLCs {
		bytes := field.ToBytes128(rlc)
		rlcLeaves[i] = bytes[:]
	}
	
	// Compute the full rlcRoot directly from all K+N leaves
	rlcTree := merkle.NewTree(rlcLeaves)
	rlcRoot := rlcTree.Root()

	return rlcRoot, nil
}

