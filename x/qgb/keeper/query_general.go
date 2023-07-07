package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// LatestUnbondingHeight queries the latest unbonding height.
func (k Keeper) LatestUnbondingHeight(
	c context.Context,
	_ *types.QueryLatestUnbondingHeightRequest,
) (*types.QueryLatestUnbondingHeightResponse, error) {
	return &types.QueryLatestUnbondingHeightResponse{
		Height: k.GetLatestUnBondingBlockHeight(sdk.UnwrapSDKContext(c)),
	}, nil
}

func (k Keeper) Params(c context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params := k.GetParams(sdk.UnwrapSDKContext(c))
	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}
