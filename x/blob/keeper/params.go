package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

// GetParams gets all parameters as types.Params
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get([]byte(types.ParamsKey))
	if len(bz) == 0 {
		// fallback to legacy store space
		// this is required because the prepare proposal handler
		// makes use of this value before the params are migrated.
		var params types.Params
		k.legacySubspace.GetParamSet(ctx, &params)
		return params
	}

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
