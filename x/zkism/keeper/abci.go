package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker contains the implementation of the appmodule.BeginBlock interface.
// This is an ABCI lifecycle method and is called at the beginning of block finalization through
// ABCI's FinalizeBlock method.
// BeginBlocker is used to track header hashes
func (k *Keeper) BeginBlocker(ctx context.Context) error {
	return k.StoreHeaderHash(ctx)
}

// StoreHeaderHash stores the header hash keyed by block height and prunes the oldest entry
func (k *Keeper) StoreHeaderHash(goCtx context.Context) error {
	ctx := sdk.UnwrapSDKContext(goCtx)

	numEntries, err := k.GetMaxHeaderHashes(ctx)
	if err != nil {
		return err
	}

	// Prune store to ensure the number of entries in store does not exceed the max header hashes parameter.
	// In most cases, this will involve removing a single entry.
	// In the rare scenario when the entries gets reduced to a lower value k'
	// from the original value k. k - k' entries must be deleted from the store.
	// Since the entries to be deleted are always in a continuous range, we can iterate
	// over the historical entries starting from the most recent version to be pruned
	// and then return at the first empty entry.
	for i := ctx.BlockHeight() - int64(numEntries); i >= 0; i-- {
		_, err := k.GetHeaderHash(ctx, uint64(i))
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				break
			}
			return err
		}

		if err = k.headers.Remove(ctx, uint64(i)); err != nil {
			return err
		}
	}

	if numEntries == 0 {
		return nil
	}

	return k.headers.Set(ctx, uint64(ctx.BlockHeight()), ctx.HeaderHash())
}
