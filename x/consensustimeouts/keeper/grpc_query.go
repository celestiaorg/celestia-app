package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Params returns the current consensustimeouts module Params.
func (k Keeper) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryParamsResponse{Params: k.GetParams(ctx)}, nil
}
