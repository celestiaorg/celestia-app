package keeper

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	k *Keeper
}

// NewQueryServerImpl creates and returns a new module QueryServer instance.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return queryServer{k}
}

// Ism implements types.QueryServer.
func (q queryServer) Ism(ctx context.Context, req *types.QueryIsmRequest) (*types.QueryIsmResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be empty")
	}

	ismId, err := util.DecodeHexAddress(req.Id)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid hex address %s, %s", req.Id, err.Error())
	}

	ism, err := q.k.isms.Get(ctx, ismId.GetInternalId())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "ism not found: %s", req.Id)
	}

	return &types.QueryIsmResponse{
		Ism: ism,
	}, nil
}

// Isms implements types.QueryServer.
func (q queryServer) Isms(ctx context.Context, req *types.QueryIsmsRequest) (*types.QueryIsmsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be empty")
	}

	transformFunc := func(_ uint64, value types.ZKExecutionISM) (types.ZKExecutionISM, error) {
		return value, nil
	}

	isms, pageRes, err := query.CollectionPaginate(ctx, q.k.isms, req.Pagination, transformFunc)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryIsmsResponse{
		Isms:       isms,
		Pagination: pageRes,
	}, nil
}

// Params implements types.QueryServer.
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument, "request cannot be empty")
	}

	params, err := q.k.params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to retrieve module params: %s", err.Error())
	}

	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}
