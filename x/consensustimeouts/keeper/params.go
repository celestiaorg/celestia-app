package keeper

import (
	"github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetParams returns the module's stored Params. If the row is missing (e.g.
// before InitGenesis has run) it falls back to types.DefaultParams.
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.ParamsKey)
	if len(bz) == 0 {
		return types.DefaultParams()
	}
	var params types.Params
	k.cdc.MustUnmarshal(bz, &params)
	return params
}

// SetParams persists the supplied Params under types.ParamsKey.
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.ParamsKey, k.cdc.MustMarshal(&params))
}
