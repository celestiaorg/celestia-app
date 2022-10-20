package keeper

import (
	"github.com/celestiaorg/celestia-app/x/payment/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetParams get all parameters as types.Params
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	return types.NewParams(
		k.MinSquareSize(ctx),
		k.MaxSqaureSize(ctx),
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

// MaxSqaureSize returns the MaxSqaureSize param
func (k Keeper) MaxSqaureSize(ctx sdk.Context) (res int32) {
	k.paramstore.Get(ctx, types.KeyMaxSqaureSize, &res)
	return
}
