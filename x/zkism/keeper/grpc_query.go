package keeper

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
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

// Ism returns a single ISM given by its id
func (q queryServer) Ism(ctx context.Context, req *types.QueryIsmRequest) (*types.QueryIsmResponse, error) {
	ismId, err := util.DecodeHexAddress(req.Id)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid hex address %s, %s", req.Id, err.Error())
	}

	ism, err := q.k.isms.Get(ctx, ismId.GetInternalId())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "ism %s not found", req.Id)
	}

	resp := &types.QueryIsmResponse{}
	switch v := ism.(type) {
	case *types.ConsensusISM:
		resp.Ism = &types.QueryIsmResponse_ConsensusIsm{ConsensusIsm: v}
	case *types.EvolveEvmISM:
		resp.Ism = &types.QueryIsmResponse_EvolveEvmIsm{EvolveEvmIsm: v}
	default:
		return nil, status.Errorf(codes.Internal, "unknown ISM type: %T", ism)
	}

	return resp, nil
}

// Isms returns all ism IDs which are registered in this module.
func (q queryServer) Isms(ctx context.Context, req *types.QueryIsmsRequest) (*types.QueryIsmsResponse, error) {
	values, pagination, err := util.GetPaginatedFromMap(ctx, q.k.isms, req.Pagination)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	ismIds := make([]string, len(values))
	for i, value := range values {
		var id util.HexAddress
		switch v := value.(type) {
		case *types.ConsensusISM:
			id, err = v.GetId()
		case *types.EvolveEvmISM:
			id, err = v.GetId()
		default:
			return nil, status.Errorf(codes.Internal, "unexpected ISM type: %T", value)
		}
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get ISM ID: %s", err.Error())
		}
		ismIds[i] = id.String()
	}

	return &types.QueryIsmsResponse{
		IsmIds:     ismIds,
		Pagination: pagination,
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
