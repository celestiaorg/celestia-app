package upgrade

import (
	"github.com/celestiaorg/celestia-app/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetParams gets all parameters as types.Params
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	return types.NewParams(
		k.SignalQuorum(ctx),
	)
}

// SetParams sets the params
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	k.paramStore.SetParamSet(ctx, &params)
}

// SignalQuorum returns the SignalQuorum param
func (k Keeper) SignalQuorum(ctx sdk.Context) (res sdk.Dec) {
	k.paramStore.Get(ctx, types.KeySignalQuorum, &res)
	return res
}
