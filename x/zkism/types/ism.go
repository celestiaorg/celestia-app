package types

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/internal/groth16"
)

const (
	InterchainSecurityModuleTypeZKExecution = 42
)

var _ ismtypes.HyperlaneInterchainSecurityModule = (*ZKExecutionISM)(nil)

// GetId implements types.HyperlaneInterchainSecurityModule.
func (ism *ZKExecutionISM) GetId() (util.HexAddress, error) {
	if ism.Id.IsZeroAddress() {
		return util.HexAddress{}, errors.New("address is empty")
	}

	return ism.Id, nil
}

// ModuleType implements types.HyperlaneInterchainSecurityModule.
func (ism *ZKExecutionISM) ModuleType() uint8 {
	return InterchainSecurityModuleTypeZKExecution
}

// Verify implements types.HyperlaneInterchainSecurityModule.
func (ism *ZKExecutionISM) Verify(ctx context.Context, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	zkProofMetadata, err := NewZkExecutionISMMetadata(metadata)
	if err != nil {
		return false, err
	}

	if zkProofMetadata.HasExecutionProof() {
		verified, err := ism.verifyZKStateTransition(zkProofMetadata)
		if err != nil || !verified {
			return false, err
		}
	}

	return ism.verifyZKStateInclusion(zkProofMetadata, message)
}

// verifyZKStateTransition verifies a ZK proof to update the ISM's state root and height.
func (ism *ZKExecutionISM) verifyZKStateTransition(metadata ZkExecutionISMMetadata) (bool, error) {
	groth16VkHash := sha256.Sum256(ism.StateTransitionVkey)
	if !bytes.Equal(groth16VkHash[:4], metadata.Proof[:4]) {
		return false, fmt.Errorf("prefix mismatch: first 4 bytes of verifier key hash (%x) do not match proof prefix (%x)", groth16VkHash[:4], metadata.Proof[:4])
	}

	proof, err := groth16.UnmarshalProof(metadata.Proof[4:])
	if err != nil {
		return false, err
	}

	vk, err := groth16.NewVerifyingKey(ism.StateTransitionVkey)
	if err != nil {
		return false, err
	}

	if err := ism.validatePublicInputs(metadata.PublicInputs); err != nil {
		return false, err
	}

	vkCommitment := new(big.Int).SetBytes(ism.VkeyCommitment)
	pubInputs, err := metadata.PublicInputs.Marshal()
	if err != nil {
		return false, err
	}

	vkElement := groth16.NewBN254FrElement(vkCommitment)
	inputsElement := groth16.NewBN254FrElement(groth16.HashBN254(pubInputs))

	pubWitness, err := groth16.NewPublicWitness(vkElement, inputsElement)
	if err != nil {
		return false, err
	}

	if err := groth16.VerifyProof(proof, vk, pubWitness); err != nil {
		return false, fmt.Errorf("failed to verify proof: %w", err)
	}

	ism.Height = metadata.PublicInputs.NewHeight
	ism.StateRoot = metadata.PublicInputs.NewStateRoot[:]

	return true, nil
}

// TODO: validate public inputs with trusted ism/celestia data
// - celestia header hash (from celestia blockchain state)
func (ism *ZKExecutionISM) validatePublicInputs(inputs PublicInputs) error {
	if !bytes.Equal(inputs.TrustedStateRoot[:], ism.StateRoot) {
		return fmt.Errorf("cannot trust public inputs trusted state root: expected %x, but got %x", ism.StateRoot, inputs.TrustedStateRoot)
	}

	if inputs.TrustedHeight != ism.Height {
		return fmt.Errorf("cannot trust public inputs trusted height: expected %d, but got %d", ism.Height, inputs.TrustedHeight)
	}

	if !bytes.Equal(inputs.Namespace[:], ism.Namespace) {
		return fmt.Errorf("cannot trust public inputs namespace: expected %x, but got %x", ism.Namespace, inputs.Namespace)
	}

	if !bytes.Equal(inputs.PublicKey[:], ism.SequencerPublicKey) {
		return fmt.Errorf("cannot trust public inputs public key: expected %x, but got %x", ism.SequencerPublicKey, inputs.PublicKey)
	}

	return nil
}

// verifyZKStateInclusion verifies merkle inclusion proofs against the current state root.
func (ism *ZKExecutionISM) verifyZKStateInclusion(_ ZkExecutionISMMetadata, _ util.HyperlaneMessage) (bool, error) {
	// TODO: https://github.com/celestiaorg/celestia-app/issues/4723
	return true, nil
}
