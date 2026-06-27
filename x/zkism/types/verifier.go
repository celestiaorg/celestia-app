package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/big"

	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v9/x/zkism/internal/groth16"
)

const (
	// PrefixLen is the number of bytes taken from the SHA-256 hash
	// of the verifying key to prefix Groth16 proofs.
	PrefixLen = 4

	// MetadataLen is the number of bytes SP1 v6 prepends to the raw Groth16
	// proof (after the vkey-hash prefix): exit_code(32) + vk_root(32) +
	// proof_nonce(32). These are public inputs to the SP1 v6 wrap circuit.
	MetadataLen = 96

	// ProofSize is the expected size in bytes of the Groth16 proof itself,
	// excluding the prefix and metadata.
	ProofSize = 256

	// ProofBytesLen is the total expected length of an SP1 v6 Groth16 proof
	// submission: PrefixLen + MetadataLen + ProofSize == 356 bytes. This is the
	// output of SP1ProofWithPublicValues::bytes() for a Groth16 proof in SP1 v6.
	ProofBytesLen = PrefixLen + MetadataLen + ProofSize
)

// vkRoot is the SP1 v6 recursion verifying-key merkle-tree root, encoded as a
// 32-byte big-endian BN254 field element. It mirrors sp1-verifier v6.2.3's
// VK_ROOT_BYTES and binds accepted proofs to the legitimate SP1 v6 recursion
// circuit. Derived from VerifierRecursionVks::default().root() (see
// sp1-verifier-6.2.3/src/lib.rs).
var vkRoot, _ = hex.DecodeString("002f850ee998974d6cc00e50cd0814b098c05bfade466d28573240d057f25352")

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
func NewSP1Groth16Verifier(vkBytes []byte) (*SP1Groth16Verifier, error) {
	vk, err := groth16.NewVerifyingKey(vkBytes)
	if err != nil {
		return nil, ErrInvalidVerifyingKey
	}

	vkHash := sha256.Sum256(vkBytes)
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
	if len(proofBz) != ProofBytesLen {
		return errorsmod.Wrapf(ErrInvalidProofLength, "expected %d, got %d", ProofBytesLen, len(proofBz))
	}

	if !bytes.Equal(v.Prefix(), proofBz[:PrefixLen]) {
		return errorsmod.Wrapf(ErrInvalidProofPrefix, "expected %x, got %x", v.Prefix(), proofBz[:PrefixLen])
	}

	// SP1 v6 layout after the 4-byte vkey-hash prefix:
	//   exit_code(32) | vk_root(32) | proof_nonce(32) | groth16 proof(256)
	exitCode := proofBz[PrefixLen : PrefixLen+32]
	vkRootBz := proofBz[PrefixLen+32 : PrefixLen+64]
	proofNonce := proofBz[PrefixLen+64 : PrefixLen+96]

	// A successful execution must have a zero exit code, and the proof must be
	// bound to the legitimate SP1 v6 recursion circuit. These mirror the checks
	// performed by sp1-verifier's Groth16Verifier::verify.
	if !bytes.Equal(exitCode, make([]byte, 32)) {
		return errorsmod.Wrapf(ErrInvalidProof, "non-zero exit code: %x", exitCode)
	}
	if !bytes.Equal(vkRootBz, vkRoot) {
		return errorsmod.Wrapf(ErrInvalidProof, "vk_root mismatch: expected %x, got %x", vkRoot, vkRootBz)
	}

	proof, err := groth16.UnmarshalProof(proofBz[PrefixLen+MetadataLen:])
	if err != nil {
		return errorsmod.Wrap(err, "failed to unmarshal proof")
	}

	// SP1 v6 commits to five public inputs, in this order:
	//   [vkey_hash, committed_values_digest, exit_code, vk_root, proof_nonce]
	vkElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(programVk))
	inputsElement := groth16.NewBN254FrElement(groth16.HashBN254(publicValues))
	exitCodeElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(exitCode))
	vkRootElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(vkRootBz))
	proofNonceElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(proofNonce))

	pubWitness, err := groth16.NewPublicWitness(vkElement, inputsElement, exitCodeElement, vkRootElement, proofNonceElement)
	if err != nil {
		return err
	}

	if err := groth16.VerifyProof(proof, v.vk, pubWitness); err != nil {
		return errorsmod.Wrapf(ErrInvalidProof, "failed to verify proof: %s", err.Error())
	}

	return nil
}
