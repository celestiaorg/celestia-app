package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
)

// HasLegacyMessageId is a test func used for checking existence of a message id in the legacy messages store collection.
func (k *Keeper) HasLegacyMessageId(ctx context.Context, ismId util.HexAddress, messageId []byte) (bool, error) {
	return k.legacyMessages.Has(ctx, collections.Join(ismId.GetInternalId(), messageId))
}

// SetLegacyMessageId is a test func used for setting a message id in the legacy messages store collection.
func (k *Keeper) SetLegacyMessageId(ctx context.Context, ismId util.HexAddress, messageId []byte) error {
	return k.legacyMessages.Set(ctx, collections.Join(ismId.GetInternalId(), messageId))
}

// GetMerkleTreeState is a test func used for getting the merkle tree state of an ISM.
func (k *Keeper) GetMerkleTreeState(ctx context.Context, ismId util.HexAddress) (types.MerkleTreeState, error) {
	return k.treeStates.Get(ctx, ismId.GetInternalId())
}

// SetMerkleTreeState is a test func used for setting the merkle tree state of an ISM.
func (k *Keeper) SetMerkleTreeState(ctx context.Context, ismId util.HexAddress, treeState types.MerkleTreeState) error {
	return k.treeStates.Set(ctx, ismId.GetInternalId(), treeState)
}

// GetMessageProofSubmitted is a test func used for getting message proof submission state.
func (k *Keeper) GetMessageProofSubmitted(ctx context.Context, ismId util.HexAddress) (bool, error) {
	return k.submissions.Get(ctx, ismId.GetInternalId())
}

// SetMessageProofSubmitted is a test func used for setting message proof submission state.
func (k *Keeper) SetMessageProofSubmitted(ctx context.Context, ismId util.HexAddress, submitted bool) error {
	return k.submissions.Set(ctx, ismId.GetInternalId(), submitted)
}

// GetIsm is a test func used for getting an ISM in the isms store collection.
func (k *Keeper) GetIsm(ctx context.Context, ismId util.HexAddress) (types.InterchainSecurityModule, error) {
	return k.isms.Get(ctx, ismId.GetInternalId())
}

// SetIsm is a test func used for setting an ISM in the isms store collection.
func (k *Keeper) SetIsm(ctx context.Context, ismId util.HexAddress, ism types.InterchainSecurityModule) error {
	return k.isms.Set(ctx, ismId.GetInternalId(), ism)
}
