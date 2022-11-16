package keeper

import (
	"github.com/celestiaorg/celestia-app/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetParams gets all parameters as types.Params
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	return types.NewParams(
		k.MinSquareSize(ctx),
		k.MaxSquareSize(ctx),
		k.GasPerBlobByte(ctx),
	)
}

// SetParams sets the params
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	k.paramStore.SetParamSet(ctx, &params)
}

// MinSquareSize returns the MinSquareSize param
func (k Keeper) MinSquareSize(ctx sdk.Context) (res uint32) {
	k.paramStore.Get(ctx, types.KeyMinSquareSize, &res)
	return
}

// MaxSquareSize returns the MaxSquareSize param
func (k Keeper) MaxSquareSize(ctx sdk.Context) (res uint32) {
	k.paramStore.Get(ctx, types.KeyMaxSquareSize, &res)
	return
}

// GasPerBlobByte returns the GasPerBlobByte param
func (k Keeper) GasPerBlobByte(ctx sdk.Context) (res uint32) {
	k.paramStore.Get(ctx, types.KeyGasPerBlobByte, &res)
	return
}
