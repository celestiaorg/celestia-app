package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ types.QueryServer = &Keeper{}

// Router returns a router by id.
func (k *Keeper) Router(ctx context.Context, req *types.QueryRouterRequest) (*types.QueryRouterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	routerID, err := util.DecodeHexAddress(req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid router id")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	router, err := k.InterchainAccountsRouters.Get(sdkCtx, routerID.GetInternalId())
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "router not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryRouterResponse{Router: router}, nil
}

// Routers returns all registered routers with pagination.
func (k *Keeper) Routers(ctx context.Context, req *types.QueryRoutersRequest) (*types.QueryRoutersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	transformFunc := func(_ uint64, value types.InterchainAccountsRouter) (types.InterchainAccountsRouter, error) {
		return value, nil
	}

	routers, pageRes, err := query.CollectionPaginate(
		ctx,
		k.InterchainAccountsRouters,
		req.Pagination,
		transformFunc,
	)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "paginate: %v", err)
	}

	return &types.QueryRoutersResponse{
		Routers:    routers,
		Pagination: pageRes,
	}, nil
}
