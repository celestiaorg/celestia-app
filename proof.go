package rsema1d

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/celestiaorg/rsema1d/encoding"
	"github.com/celestiaorg/rsema1d/field"
	"github.com/celestiaorg/rsema1d/merkle"
)


// CreateVerificationContext initializes context with RLC original values
// Used for DA sampling with multiple proofs
func CreateVerificationContext(rlcOrig []field.GF128, config *Config) (*VerificationContext, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	
	if len(rlcOrig) != config.K {
		return nil, fmt.Errorf("expected %d RLC values, got %d", config.K, len(rlcOrig))
	}
	
	// Extend RLC results to get all K+N values
	rlcExtended, err := encoding.ExtendRLCResults(rlcOrig, config.N)
	if err != nil {
		return nil, fmt.Errorf("failed to extend RLC results: %w", err)
	}
	
	// Build padded RLC Merkle tree
	rlcTree := buildPaddedRLCTree(rlcExtended, config)
	
	return &VerificationContext{
		config:      config,
		rlcOrig:     rlcOrig,
		rlcExtended: rlcExtended,
		rlcTree:     rlcTree,
		rlcRoot:     rlcTree.Root(),
	}, nil
}

// VerifyRowWithContext verifies a row proof using pre-initialized context
// Efficient for multiple verifications with same commitment
func VerifyRowWithContext(proof *RowProof, commitment Commitment, context *VerificationContext) error {
	if proof.Index < 0 || proof.Index >= context.config.K+context.config.N {
		return fmt.Errorf("index %d out of range [0, %d)", proof.Index, context.config.K+context.config.N)
	}
	
	// 1. Compute row root from proof (using mapped tree position)
	treeIndex := mapIndexToTreePosition(proof.Index, context.config)
	rowRoot, err := merkle.ComputeRootFromProof(proof.Row, treeIndex, proof.RowProof)
	if err != nil {
		return fmt.Errorf("failed to compute row root: %w", err)
	}
	
	// 2. Derive coefficients and compute RLC for the row
	coeffs := deriveCoefficients(rowRoot, context.config)
	computedRLC := computeRLC(proof.Row, coeffs, context.config)
	
	// 3. Verify RLC matches the extended value at this index
	if proof.Index >= len(context.rlcExtended) {
		return fmt.Errorf("index %d out of range", proof.Index)
	}
	
	expectedRLC := context.rlcExtended[proof.Index]
	if !field.Equal128(computedRLC, expectedRLC) {
		return errors.New("computed RLC does not match expected value")
	}
	
	// 4. Verify commitment
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(context.rlcRoot[:])
	computedCommitment := h.Sum(nil)
	
	if commitment != [32]byte(computedCommitment) {
		return errors.New("commitment verification failed")
	}
	
	return nil
}

// VerifyStandaloneProof verifies a self-contained proof without context
// Best for single row verification without downloading RLC orig
func VerifyStandaloneProof(proof *StandaloneProof, commitment Commitment, config *Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	
	if proof.Index >= config.K {
		return errors.New("standalone verification only supports original rows")
	}
	
	// 1. Compute row root (index < K so no shift needed for tree position)
	rowRoot, err := merkle.ComputeRootFromProof(proof.Row, proof.Index, proof.RowProof.RowProof)
	if err != nil {
		return fmt.Errorf("failed to compute row root: %w", err)
	}
	
	// 2. Compute RLC for the row
	coeffs := deriveCoefficients(rowRoot, config)
	computedRLC := computeRLC(proof.Row, coeffs, config)
	
	// 3. Compute RLC root from proof
	rlcBytes := field.ToBytes128(computedRLC)
	rlcRoot, err := merkle.ComputeRootFromProof(rlcBytes[:], proof.Index, proof.RLCProof)
	if err != nil {
		return fmt.Errorf("failed to compute RLC root: %w", err)
	}
	
	// 4. Verify commitment
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcRoot[:])
	computedCommitment := h.Sum(nil)
	
	if commitment != [32]byte(computedCommitment) {
		return errors.New("commitment verification failed")
	}
	
	return nil
}

