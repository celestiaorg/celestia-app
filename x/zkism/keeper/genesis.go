package keeper

import (
	"context"
	"math"
	"sort"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// InitGenesis initialises the module genesis state. Proof heights only serve the one-proof-per-block rule and the
// history window, so heights at or after the first block of this chain (e.g. after a zero-height export) are lowered
// below it; otherwise the first proof at that height would be rejected or dropped.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	firstHeight := max(sdk.UnwrapSDKContext(ctx).BlockHeight(), 1)
	for _, ism := range gs.Isms {
		ism.StateHeight = clampGenesisHeight(ism.StateHeight, firstHeight)
		if err := k.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
			return err
		}
	}

	for _, entry := range gs.TreeStates {
		ismId := entry.Id
		exists, err := k.isms.Has(ctx, ismId.GetInternalId())
		if err != nil {
			return err
		}
		if !exists {
			return errorsmod.Wrapf(types.ErrIsmNotFound, "merkle tree state defined for unknown ism %s", ismId.String())
		}

		entry.TreeState.Height = clampGenesisHeight(entry.TreeState.Height, firstHeight)
		if err := k.treeStates.Set(ctx, ismId.GetInternalId(), entry.TreeState); err != nil {
			return err
		}
	}

	// Submissions are no longer tracked; the deprecated genesis field is ignored.
	if err := k.importRootHistory(ctx, k.stateRootHistory, gs.StateRootHistory, firstHeight); err != nil {
		return err
	}

	return k.importRootHistory(ctx, k.treeStateHistory, gs.TreeStateHistory, firstHeight)
}

// clampGenesisHeight lowers an imported proof height to the block before the first block of this chain.
func clampGenesisHeight(height, firstHeight int64) int64 {
	if height >= firstHeight {
		return firstHeight - 1
	}
	return height
}

// importRootHistory writes genesis root history entries, rejecting entries for unknown isms.
func (k *Keeper) importRootHistory(ctx context.Context, history rootHistory, entries []types.GenesisRootHistoryEntry, firstHeight int64) error {
	for _, entry := range entries {
		exists, err := k.isms.Has(ctx, entry.Id.GetInternalId())
		if err != nil {
			return err
		}
		if !exists {
			return errorsmod.Wrapf(types.ErrIsmNotFound, "root history defined for unknown ism %s", entry.Id.String())
		}

		// InitChain does not run genesis validation, so guard the bound that keeps pushHistory from wrapping here too.
		if entry.Sequence > math.MaxInt64 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "root history sequence %d for ism %s exceeds %d", entry.Sequence, entry.Id.String(), int64(math.MaxInt64))
		}

		entry.Entry.Height = clampGenesisHeight(entry.Entry.Height, firstHeight)

		if err := history.Set(ctx, collections.Join(entry.Id.GetInternalId(), entry.Sequence), entry.Entry); err != nil {
			return err
		}
	}
	return nil
}

// ExportGenesis outputs the modules state for genesis exports.
func (k *Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var isms []types.InterchainSecurityModule
	if err := k.isms.Walk(ctx, nil, func(_ uint64, value types.InterchainSecurityModule) (bool, error) {
		isms = append(isms, value)
		return false, nil
	}); err != nil {
		return nil, err
	}

	ismByID := make(map[uint64]util.HexAddress, len(isms))
	for i := range isms {
		ism := isms[i]
		ismByID[ism.Id.GetInternalId()] = ism.Id
	}

	treeStates := make([]types.GenesisMerkleTreeState, 0)
	if err := k.treeStates.Walk(ctx, nil, func(key uint64, value types.MerkleTreeState) (bool, error) {
		ismId, ok := ismByID[key]
		if !ok {
			return false, errorsmod.Wrapf(types.ErrIsmNotFound, "merkle tree state entry for unknown ism %d", key)
		}

		treeStates = append(treeStates, types.GenesisMerkleTreeState{
			Id:        ismId,
			TreeState: value,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}

	sort.Slice(treeStates, func(i, j int) bool {
		return treeStates[i].Id.Compare(treeStates[j].Id) < 0
	})

	stateRootHistory, err := exportRootHistory(ctx, k.stateRootHistory, ismByID)
	if err != nil {
		return nil, err
	}

	treeStateHistory, err := exportRootHistory(ctx, k.treeStateHistory, ismByID)
	if err != nil {
		return nil, err
	}

	return &types.GenesisState{
		Isms:             isms,
		TreeStates:       treeStates,
		StateRootHistory: stateRootHistory,
		TreeStateHistory: treeStateHistory,
	}, nil
}

// exportRootHistory returns every root history entry, ordered by ism and then by sequence (the store key order).
func exportRootHistory(ctx context.Context, history rootHistory, ismByID map[uint64]util.HexAddress) ([]types.GenesisRootHistoryEntry, error) {
	entries := make([]types.GenesisRootHistoryEntry, 0)
	err := history.Walk(ctx, nil, func(key collections.Pair[uint64, uint64], value types.RootHistoryEntry) (bool, error) {
		ismId, ok := ismByID[key.K1()]
		if !ok {
			return false, errorsmod.Wrapf(types.ErrIsmNotFound, "root history entry for unknown ism %d", key.K1())
		}

		entries = append(entries, types.GenesisRootHistoryEntry{
			Id:       ismId,
			Sequence: key.K2(),
			Entry:    value,
		})
		return false, nil
	})
	return entries, err
}
