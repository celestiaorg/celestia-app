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

// InitGenesis initialises the module genesis state. The deprecated submissions field is ignored.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	// Proof heights from an exported chain may be at or above this chain's first block, where the per-block rule
	// would reject a proof in the block with that height. Lower them to the block before.
	lastHeight := max(sdk.UnwrapSDKContext(ctx).BlockHeight(), 1) - 1

	for _, ism := range gs.Isms {
		ism.StateHeight = min(ism.StateHeight, lastHeight)
		if err := k.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
			return err
		}
	}

	for _, entry := range gs.TreeStates {
		if err := k.requireIsm(ctx, entry.Id); err != nil {
			return err
		}

		entry.TreeState.Height = min(entry.TreeState.Height, lastHeight)
		if err := k.treeStates.Set(ctx, entry.Id.GetInternalId(), entry.TreeState); err != nil {
			return err
		}
	}

	if err := k.importRootHistory(ctx, k.stateRootHistory, gs.StateRootHistory); err != nil {
		return err
	}
	return k.importRootHistory(ctx, k.treeStateHistory, gs.TreeStateHistory)
}

// importRootHistory writes genesis root history entries.
func (k *Keeper) importRootHistory(ctx context.Context, history rootHistory, entries []types.GenesisRootHistoryEntry) error {
	for _, entry := range entries {
		if err := k.requireIsm(ctx, entry.Id); err != nil {
			return err
		}

		// InitChain does not run genesis validation, so check the bound that keeps sequences from wrapping here.
		if entry.Sequence > math.MaxInt64 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "root history sequence %d for ism %s exceeds %d", entry.Sequence, entry.Id.String(), int64(math.MaxInt64))
		}

		if err := history.Set(ctx, collections.Join(entry.Id.GetInternalId(), entry.Sequence), entry.Entry); err != nil {
			return err
		}
	}
	return nil
}

// requireIsm returns ErrIsmNotFound if the ism does not exist.
func (k *Keeper) requireIsm(ctx context.Context, id util.HexAddress) error {
	exists, err := k.isms.Has(ctx, id.GetInternalId())
	if err != nil {
		return err
	}
	if !exists {
		return errorsmod.Wrapf(types.ErrIsmNotFound, "genesis entry for unknown ism %s", id.String())
	}
	return nil
}

// ExportGenesis outputs the modules state for genesis exports.
func (k *Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var isms []types.InterchainSecurityModule
	ismByID := make(map[uint64]util.HexAddress)
	if err := k.isms.Walk(ctx, nil, func(key uint64, value types.InterchainSecurityModule) (bool, error) {
		isms = append(isms, value)
		ismByID[key] = value.Id
		return false, nil
	}); err != nil {
		return nil, err
	}

	treeStates := make([]types.GenesisMerkleTreeState, 0)
	if err := k.treeStates.Walk(ctx, nil, func(key uint64, value types.MerkleTreeState) (bool, error) {
		ismId, ok := ismByID[key]
		if !ok {
			return false, errorsmod.Wrapf(types.ErrIsmNotFound, "merkle tree state entry for unknown ism %d", key)
		}

		treeStates = append(treeStates, types.GenesisMerkleTreeState{Id: ismId, TreeState: value})
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

// exportRootHistory returns every root history entry in store key order.
func exportRootHistory(ctx context.Context, history rootHistory, ismByID map[uint64]util.HexAddress) ([]types.GenesisRootHistoryEntry, error) {
	entries := make([]types.GenesisRootHistoryEntry, 0)
	err := history.Walk(ctx, nil, func(key collections.Pair[uint64, uint64], value types.RootHistoryEntry) (bool, error) {
		ismId, ok := ismByID[key.K1()]
		if !ok {
			return false, errorsmod.Wrapf(types.ErrIsmNotFound, "root history entry for unknown ism %d", key.K1())
		}

		entries = append(entries, types.GenesisRootHistoryEntry{Id: ismId, Sequence: key.K2(), Entry: value})
		return false, nil
	})
	return entries, err
}
