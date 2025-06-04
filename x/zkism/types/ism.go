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

	"github.com/celestiaorg/celestia-app/v4/x/zkism/internal/groth16"
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
		verified, err := ism.verifyZKProof(zkProofMetadata)
		if err != nil || !verified {
			return false, err
		}
	}

	return ism.verifyMerkleProofs(zkProofMetadata, message)
}

// verifyZKProof verifies a ZK proof to update the ISM's state root and height.
func (ism *ZKExecutionISM) verifyZKProof(metadata ZkExecutionISMMetadata) (bool, error) {
	groth16VkHash := sha256.Sum256(ism.StateTransitionVerifierKey)
	if !bytes.Equal(groth16VkHash[:4], metadata.Proof[:4]) {
		return false, fmt.Errorf("prefix mismatch: first 4 bytes of verifier key hash (%x) do not match proof prefix (%x)", groth16VkHash[:4], metadata.Proof[:4])
	}

	proof, err := groth16.UnmarshalProof(metadata.Proof[4:])
	if err != nil {
		return false, err
	}

	vk, err := groth16.NewVerifyingKey(ism.StateTransitionVerifierKey)
	if err != nil {
		return false, err
	}

	// TODO: We should store the sp1 verifier key on the ism.
	// For now lets just take the unused state membership bz here to satisfy the compiler
	vkHash := sha256.Sum256(ism.StateMembershipVerifierKey)
	vkHashBigInt := new(big.Int).SetBytes(vkHash[:])

	// TODO: Find out and validate what exactly are the public inputs, how they should be received and decoded.
	// The public inputs returned from the evm aggregration sp1 program are currently defined here:
	// https://github.com/celestiaorg/celestia-zkevm-ibc-demo/blob/main/provers/blevm/common/src/lib.rs#L16
	//
	// We must be able to parse the public input bytes in order to update the ism height and state root.
	// We should be able to just read n bytes from a single public inputs byte slice returned from the prover service.
	// This can be done as part of metadata parsing.
	// For now just take index 0 here to satisfy compiler and put code in place.
	sp1PubInputs := metadata.PublicInputs[0]
	sp1PubInputsHash := groth16.HashBN254(sp1PubInputs)

	vkElement := groth16.NewBN254FrElement(vkHashBigInt)
	pubInputsElement := groth16.NewBN254FrElement(sp1PubInputsHash)

	pubWitness, err := groth16.NewPublicWitness(vkElement, pubInputsElement)
	if err != nil {
		return false, err
	}

	if err := groth16.VerifyProof(proof, vk, pubWitness); err != nil {
		return false, err
	}

	// TODO: once the proof is verified, update the height and state root on the ism from the public inputs
	return true, nil
}

// verifyMerkleProofs verifies merkle inclusion proofs against the current state root.
func (ism *ZKExecutionISM) verifyMerkleProofs(_ ZkExecutionISMMetadata, _ util.HyperlaneMessage) (bool, error) {
	// TODO: https://github.com/celestiaorg/celestia-app/issues/4723
	return true, nil
}
