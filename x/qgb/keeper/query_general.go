package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// LastUnbondingHeight queries the last unbonding height.
func (k Keeper) LastUnbondingHeight(
	c context.Context,
	_ *types.QueryLastUnbondingHeightRequest,
) (*types.QueryLastUnbondingHeightResponse, error) {
	return &types.QueryLastUnbondingHeightResponse{
		Height: k.GetLastUnBondingBlockHeight(sdk.UnwrapSDKContext(c)),
	}, nil
}

func (k Keeper) Params(c context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params := k.GetParams(sdk.UnwrapSDKContext(c))
	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}
