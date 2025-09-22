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
// TODO: follow up PR, refactor/remove this code from here
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
	groth16VkHash := sha256.Sum256(ism.Groth16Vkey)
	if !bytes.Equal(groth16VkHash[:4], metadata.Proof[:4]) {
		return false, fmt.Errorf("prefix mismatch: first 4 bytes of verifier key hash (%x) do not match proof prefix (%x)", groth16VkHash[:4], metadata.Proof[:4])
	}

	proof, err := groth16.UnmarshalProof(metadata.Proof[4:])
	if err != nil {
		return false, err
	}

	vk, err := groth16.NewVerifyingKey(ism.Groth16Vkey)
	if err != nil {
		return false, err
	}

	vkCommitment := new(big.Int).SetBytes(ism.StateTransitionVkey)
	pubVals, err := metadata.PublicValues.Marshal()
	if err != nil {
		return false, err
	}

	vkElement := groth16.NewBN254FrElement(vkCommitment)
	inputsElement := groth16.NewBN254FrElement(groth16.HashBN254(pubVals))

	pubWitness, err := groth16.NewPublicWitness(vkElement, inputsElement)
	if err != nil {
		return false, err
	}

	if err := groth16.VerifyProof(proof, vk, pubWitness); err != nil {
		return false, fmt.Errorf("failed to verify proof: %w", err)
	}

	return true, nil
}

// verifyZKStateInclusion verifies merkle inclusion proofs against the current state root.
func (ism *ZKExecutionISM) verifyZKStateInclusion(_ ZkExecutionISMMetadata, _ util.HyperlaneMessage) (bool, error) {
	// TODO: https://github.com/celestiaorg/celestia-app/issues/4723
	return true, nil
}
