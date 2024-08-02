package keeper

import (
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetParams gets all parameters as types.Params
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	return types.NewParams(
		k.GasPerBlobByte(ctx),
		k.GovMaxSquareSize(ctx),
	)
}

// SetParams sets the params
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	k.paramStore.SetParamSet(ctx, &params)
}

// GasPerBlobByte returns the GasPerBlobByte param
func (k Keeper) GasPerBlobByte(ctx sdk.Context) (res uint32) {
	k.paramStore.Get(ctx, types.KeyGasPerBlobByte, &res)
	return res
}

// GovMaxSquareSize returns the GovMaxSquareSize param
func (k Keeper) GovMaxSquareSize(ctx sdk.Context) (res uint64) {
	k.paramStore.Get(ctx, types.KeyGovMaxSquareSize, &res)
	return res
}
