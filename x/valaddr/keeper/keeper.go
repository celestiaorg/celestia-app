package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/core/store"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v9/x/valaddr/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// Keeper of the valaddr store
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	logger       log.Logger

	stakingKeeper types.StakingKeeper
}

// NewKeeper creates a new valaddr Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	logger log.Logger,
	stakingKeeper types.StakingKeeper,
) Keeper {
	return Keeper{
		cdc:           cdc,
		storeService:  storeService,
		logger:        logger,
		stakingKeeper: stakingKeeper,
	}
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx context.Context) log.Logger {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return k.logger.With("module", "x/"+types.ModuleName, "height", sdkCtx.BlockHeight())
}

// SetFibreProviderInfo sets the fibre provider info for a validator
func (k Keeper) SetFibreProviderInfo(ctx context.Context, consAddr sdk.ConsAddress, info types.FibreProviderInfo) error {
	store := k.storeService.OpenKVStore(ctx)
	key := types.GetFibreProviderInfoKey(consAddr)
	bz, err := k.cdc.Marshal(&info)
	if err != nil {
		return err
	}
	return store.Set(key, bz)
}

// GetFibreProviderInfo retrieves the fibre provider info for a validator
func (k Keeper) GetFibreProviderInfo(ctx context.Context, consAddr sdk.ConsAddress) (types.FibreProviderInfo, bool) {
	store := k.storeService.OpenKVStore(ctx)
	key := types.GetFibreProviderInfoKey(consAddr)

	bz, err := store.Get(key)
	if err != nil || bz == nil {
		return types.FibreProviderInfo{}, false
	}

	var info types.FibreProviderInfo
	if err := k.cdc.Unmarshal(bz, &info); err != nil {
		return types.FibreProviderInfo{}, false
	}

	return info, true
}

// DeleteFibreProviderInfo deletes the fibre provider info for a validator
func (k Keeper) DeleteFibreProviderInfo(ctx context.Context, consAddr sdk.ConsAddress) error {
	store := k.storeService.OpenKVStore(ctx)
	key := types.GetFibreProviderInfoKey(consAddr)
	return store.Delete(key)
}

// RemoveStaleFibreProviders garbage-collects FibreProviderInfo entries whose
// validator has permanently left the active set: either fully removed from
// staking state, or jailed and unbonded for longer than
// JailedGracePeriod(1 month).
//
// A validator lookup that fails with anything other than ErrNoValidatorFound
// indicates a staking state-read problem; in that case the sweep aborts and
// returns the error rather than acting on partial state.
func (k Keeper) RemoveStaleFibreProviders(ctx context.Context) error {
	blockTime := sdk.UnwrapSDKContext(ctx).BlockTime()

	var stale []sdk.ConsAddress
	var lookupErr error
	err := k.IterateFibreProviderInfo(ctx, func(consAddr sdk.ConsAddress, _ types.FibreProviderInfo) bool {
		validator, err := k.stakingKeeper.GetValidatorByConsAddr(ctx, consAddr)
		if err != nil {
			// A definitively-removed validator is stale and its entry is dropped.
			if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
				stale = append(stale, copyConsAddr(consAddr))
				return false
			}
			// Any other error means staking state could not be read. Something
			// is off, so abort the sweep instead of risking action on partial
			// state.
			lookupErr = fmt.Errorf("looking up validator %s: %w", consAddr, err)
			return true
		}

		// A validator that has been jailed and has finished unbonding without
		// recovering for longer than the grace period is treated as gone.
		if validator.IsJailed() && !validator.IsBonded() &&
			blockTime.After(validator.UnbondingTime.Add(types.JailedGracePeriod)) {
			stale = append(stale, copyConsAddr(consAddr))
		}
		return false
	})
	if err != nil {
		return err
	}
	if lookupErr != nil {
		return lookupErr
	}

	for _, consAddr := range stale {
		if err := k.DeleteFibreProviderInfo(ctx, consAddr); err != nil {
			return err
		}
	}
	return nil
}

// copyConsAddr returns a copy of the consensus address. The address handed to
// the iterator callback aliases iterator-owned memory that is reused on the
// next iteration, so it must be copied before being retained.
func copyConsAddr(consAddr sdk.ConsAddress) sdk.ConsAddress {
	out := make(sdk.ConsAddress, len(consAddr))
	copy(out, consAddr)
	return out
}

// IterateFibreProviderInfo iterates over all fibre provider info entries
func (k Keeper) IterateFibreProviderInfo(ctx context.Context, cb func(consAddr sdk.ConsAddress, info types.FibreProviderInfo) bool) error {
	store := k.storeService.OpenKVStore(ctx)

	iterator, err := store.Iterator(
		types.FibreProviderInfoPrefix,
		storetypes.PrefixEndBytes(types.FibreProviderInfoPrefix),
	)
	if err != nil {
		return err
	}
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var info types.FibreProviderInfo
		if err := k.cdc.Unmarshal(iterator.Value(), &info); err != nil {
			return err
		}

		// Extract consensus address from key (skip the prefix byte)
		consAddr := sdk.ConsAddress(iterator.Key()[len(types.FibreProviderInfoPrefix):])

		if cb(consAddr, info) {
			break
		}
	}

	return nil
}
