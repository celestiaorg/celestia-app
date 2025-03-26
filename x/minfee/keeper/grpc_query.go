package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/celestiaorg/celestia-app/v4/x/minfee/types"
)

var _ types.QueryServer = &Keeper{}

// NetworkMinGasPrice returns the network minimum gas price.
func (k *Keeper) NetworkMinGasPrice(ctx context.Context, _ *types.QueryNetworkMinGasPrice) (*types.QueryNetworkMinGasPriceResponse, error) {
	// delegate to the self managed params.
	networkMinGasPrice := k.GetParams(sdk.UnwrapSDKContext(ctx)).NetworkMinGasPrice

	// TODO: do we need to fallback to the subspace method for v3 and below?

	return &types.QueryNetworkMinGasPriceResponse{NetworkMinGasPrice: networkMinGasPrice}, nil
}

func (k Keeper) Params(c context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(c)
	return &types.QueryParamsResponse{Params: k.GetParams(ctx)}, nil
}
