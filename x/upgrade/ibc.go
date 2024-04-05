package upgrade

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/upgrade/types"
	ibctypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
)

// We need compatibility with the way that IBC uses the upgrade module. This file
// ensures that we comply to the interface that IBC expects.
var _ ibctypes.UpgradeKeeper = (*Keeper)(nil)

// ScheduleUpgrade implements the IBC upgrade keeper interface. This is a noop as
// no other process is allowed to schedule an upgrade but the upgrade keeper itself.
// This is kept around to support the interface.
func (k Keeper) ScheduleUpgrade(_ sdk.Context, _ types.Plan) error {
	return nil
}

// GetUpgradePlan implements the IBC upgrade keeper interface. This is used in BeginBlock
// to know when to write the upgraded consensus state. The IBC module needs to sign over
// the next consensus state to ensure a smooth transition for counterparty chains. This
// is implemented as a noop. Any IBC breaking change would be invoked by this upgrade module
// in end blocker.
func (k Keeper) GetUpgradePlan(_ sdk.Context) (plan types.Plan, havePlan bool) {
	return types.Plan{}, false
}

// SetUpgradedClient sets the expected upgraded client for the next version of
// this chain at the last height the current chain will commit.
func (k Keeper) SetUpgradedClient(ctx sdk.Context, planHeight int64, bz []byte) error {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.UpgradedClientKey(planHeight), bz)
	return nil
}

// GetUpgradedClient gets the expected upgraded client for the next version of
// this chain.
func (k Keeper) GetUpgradedClient(ctx sdk.Context, height int64) ([]byte, bool) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.UpgradedClientKey(height))
	if len(bz) == 0 {
		return nil, false
	}

	return bz, true
}

// SetUpgradedConsensusState sets the expected upgraded consensus state for the
// next version of this chain using the last height committed on this chain.
func (k Keeper) SetUpgradedConsensusState(ctx sdk.Context, planHeight int64, bz []byte) error {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.UpgradedConsStateKey(planHeight), bz)
	return nil
}

// GetUpgradedConsensusState gets the expected upgraded consensus state for the
// next version of this chain.
func (k Keeper) GetUpgradedConsensusState(ctx sdk.Context, lastHeight int64) ([]byte, bool) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.UpgradedConsStateKey(lastHeight))
	if len(bz) == 0 {
		return nil, false
	}

	return bz, true
}

// ClearIBCState clears any planned IBC state.
func (k Keeper) ClearIBCState(ctx sdk.Context, lastHeight int64) {
	// delete IBC client and consensus state from store if this is IBC plan
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.UpgradedClientKey(lastHeight))
	store.Delete(types.UpgradedConsStateKey(lastHeight))
}
