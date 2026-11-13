package keeper

import (
	"github.com/celestiaorg/celestia-app/v11/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetParams gets all parameters as types.Params. InitGenesis, the blob 2 -> 3
// migration and the v11 upgrade (Migrator.EnsureParams) guarantee that the
// store has them.
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get([]byte(types.ParamsKey))
	var params types.Params
	k.cdc.MustUnmarshal(bz, &params)
	return params
}

// SetParams sets the params
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&params)
	store.Set([]byte(types.ParamsKey), bz)
}

// SetParamsLegacy sets the params in the legacy store space.
// TODO: this can be removed in versions after migrations have run.
func (k Keeper) SetParamsLegacy(ctx sdk.Context, params types.Params) {
	k.legacySubspace.SetParamSet(ctx, &params)
}
