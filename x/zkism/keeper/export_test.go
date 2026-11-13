package keeper

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
)

// GetMerkleTreeState is a test func used for getting the merkle tree state of an ISM.
func (k *Keeper) GetMerkleTreeState(ctx context.Context, ismId util.HexAddress) (types.MerkleTreeState, error) {
	return k.treeStates.Get(ctx, ismId.GetInternalId())
}

// SetMerkleTreeState is a test func used for setting the merkle tree state of an ISM.
func (k *Keeper) SetMerkleTreeState(ctx context.Context, ismId util.HexAddress, treeState types.MerkleTreeState) error {
	return k.treeStates.Set(ctx, ismId.GetInternalId(), treeState)
}

// PushStateRootHistory is a test func used for recording a replaced state root of an ISM.
func (k *Keeper) PushStateRootHistory(ctx context.Context, ism types.InterchainSecurityModule, root []byte) error {
	entry := types.RootHistoryEntry{Root: root}
	return pushHistory(ctx, k.stateRootHistory, ism.Id.GetInternalId(), types.EffectiveRootHistorySize(ism.RootHistorySize), entry)
}

// PushTreeStateHistory is a test func used for recording a replaced merkle tree state of an ISM.
func (k *Keeper) PushTreeStateHistory(ctx context.Context, ism types.InterchainSecurityModule, treeState types.MerkleTreeState) error {
	entry := types.RootHistoryEntry{Root: treeState.Root, Count: treeState.Count}
	return pushHistory(ctx, k.treeStateHistory, ism.Id.GetInternalId(), types.EffectiveRootHistorySize(ism.RootHistorySize), entry)
}

// GetStateRootHistory is a test func used for getting the state root history of an ISM, oldest first.
func (k *Keeper) GetStateRootHistory(ctx context.Context, ismId util.HexAddress) ([]types.RootHistoryEntry, error) {
	return historyEntries(ctx, k.stateRootHistory, ismId.GetInternalId())
}

// GetTreeStateHistory is a test func used for getting the merkle tree state history of an ISM, oldest first.
func (k *Keeper) GetTreeStateHistory(ctx context.Context, ismId util.HexAddress) ([]types.RootHistoryEntry, error) {
	return historyEntries(ctx, k.treeStateHistory, ismId.GetInternalId())
}

// GetIsm is a test func used for getting an ISM in the isms store collection.
func (k *Keeper) GetIsm(ctx context.Context, ismId util.HexAddress) (types.InterchainSecurityModule, error) {
	return k.isms.Get(ctx, ismId.GetInternalId())
}

// SetIsm is a test func used for setting an ISM in the isms store collection.
func (k *Keeper) SetIsm(ctx context.Context, ismId util.HexAddress, ism types.InterchainSecurityModule) error {
	return k.isms.Set(ctx, ismId.GetInternalId(), ism)
}
