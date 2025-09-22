package types

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/big"

	"github.com/celestiaorg/celestia-app/v6/x/zkism/internal/groth16"
)

const (
	// PrefixLen is the number of bytes taken from the SHA-256 hash
	// of the verifying key to prefix Groth16 proofs.
	PrefixLen = 4

	// ProofSize is the expected size in bytes of the Groth16 proof itself,
	// excluding the prefix.
	ProofSize = 256
)

// SP1Groth16Verifier encapsulates the state required to verify Groth16 proofs
// under the SP1 scheme. It stores a verifying key and its hash prefix, which
// are used to check proof integrity and correctness.
type SP1Groth16Verifier struct {
	prefix [PrefixLen]byte
	vk     groth16.VerifyingKey
}

// NewSP1Groth16Verifier constructs a new SP1Groth16Verifier from the provided
// verifying key bytes. It initializes the internal verifying key and computes
// the hash prefix used to validate proofs.
//
// Returns an error if the verifying key cannot be parsed.
func NewSP1Groth16Verifier(groth16Vk []byte) (*SP1Groth16Verifier, error) {
	vk, err := groth16.NewVerifyingKey(groth16Vk)
	if err != nil {
		return nil, fmt.Errorf("new verifying key: %w", err)
	}

	vkHash := sha256.Sum256(groth16Vk)
	var prefix [PrefixLen]byte
	copy(prefix[:], vkHash[:PrefixLen])

	return &SP1Groth16Verifier{
		prefix: prefix,
		vk:     vk,
	}, nil
}

// Prefix returns the verifier's SP1 hash prefix.
func (v *SP1Groth16Verifier) Prefix() []byte {
	return v.prefix[:]
}

// VerifyProof checks that the given proof is valid using the verifier's key,
// the provided program verifying key commitment, and the public values.
// The proof must be prefixed with the verifier key hash prefix.
// Returns nil if the proof is valid, or an error otherwise.
func (v *SP1Groth16Verifier) VerifyProof(proofBz, programVk, publicValues []byte) error {
	if len(proofBz) != (PrefixLen + ProofSize) {
		return fmt.Errorf("proof too short: expected %d, got %d", len(proofBz), (PrefixLen + ProofSize))
	}

	if !bytes.Equal(v.Prefix(), proofBz[:PrefixLen]) {
		return fmt.Errorf("invalid proof prefix expected %x, got %x", v.prefix[:], proofBz[:PrefixLen])
	}

	proof, err := groth16.UnmarshalProof(proofBz[PrefixLen:])
	if err != nil {
		return fmt.Errorf("unmarshal proof: %w", err)
	}

	vkCommitment := new(big.Int).SetBytes(programVk)
	vkElement := groth16.NewBN254FrElement(vkCommitment)
	inputsElement := groth16.NewBN254FrElement(groth16.HashBN254(publicValues))

	pubWitness, err := groth16.NewPublicWitness(vkElement, inputsElement)
	if err != nil {
		return err
	}

	if err := groth16.VerifyProof(proof, v.vk, pubWitness); err != nil {
		return fmt.Errorf("verify proof: %w", err)
	}

	return nil
}
