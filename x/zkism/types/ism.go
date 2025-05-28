package types

import (
	"context"
	"errors"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
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
func (ism *ZKExecutionISM) verifyZKProof(_ ZkExecutionISMMetadata) (bool, error) {
	// TODO: https://github.com/celestiaorg/celestia-app/issues/4723
	return true, nil
}

// verifyMerkleProofs verifies merkle inclusion proofs against the current state root.
func (ism *ZKExecutionISM) verifyMerkleProofs(_ ZkExecutionISMMetadata, _ util.HyperlaneMessage) (bool, error) {
	// TODO: https://github.com/celestiaorg/celestia-app/issues/4723
	return true, nil
}
