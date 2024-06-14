package minfee

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/params/keeper"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ QueryServer = &QueryServerImpl{}

// QueryServerImpl wraps the params keeper and implements the minfee gRPC query server.
type QueryServerImpl struct {
	paramsKeeper keeper.Keeper
}

// NewQueryServerImpl creates a new QueryServerImpl.
func NewQueryServerImpl(paramsKeeper keeper.Keeper) *QueryServerImpl {
	return &QueryServerImpl{paramsKeeper: paramsKeeper}
}

// NetworkMinGasPrice returns the network minimum gas price.
func (q *QueryServerImpl) NetworkMinGasPrice(ctx context.Context, _ *QueryNetworkMinGasPrice) (*QueryNetworkMinGasPriceResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	var params Params
	subspace, found := q.paramsKeeper.GetSubspace(ModuleName)
	if !found {
		return nil, status.Errorf(codes.NotFound, "subspace not found for minfee. Minfee is only active in app version 2 and onwards")
	}
	subspace.GetParamSet(sdkCtx, &params)
	return &QueryNetworkMinGasPriceResponse{NetworkMinGasPrice: params.GlobalMinGasPrice}, nil
}
