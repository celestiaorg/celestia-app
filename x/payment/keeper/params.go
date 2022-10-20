package keeper

import (
	"github.com/celestiaorg/celestia-app/x/payment/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetParams get all parameters as types.Params
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	return types.NewParams(
		k.MinSquareSize(ctx),
		k.MaxSquareSize(ctx),
	)
}

// SetParams set the params
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	k.paramstore.SetParamSet(ctx, &params)
}

// MinSquareSize returns the MinSquareSize param
func (k Keeper) MinSquareSize(ctx sdk.Context) (res int32) {
	k.paramstore.Get(ctx, types.KeyMinSquareSize, &res)
	return
}

// MaxSquareSize returns the MaxSquareSize param
func (k Keeper) MaxSquareSize(ctx sdk.Context) (res int32) {
	k.paramstore.Get(ctx, types.KeyMaxSquareSize, &res)
	return
}
