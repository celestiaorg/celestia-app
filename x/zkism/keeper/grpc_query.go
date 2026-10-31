package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
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

	transformFunc := func(_ uint64, value types.InterchainSecurityModule) (types.InterchainSecurityModule, error) {
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

// MerkleTreeState implements types.QueryServer.
func (q queryServer) MerkleTreeState(ctx context.Context, req *types.QueryMerkleTreeStateRequest) (*types.QueryMerkleTreeStateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be empty")
	}

	ismId, err := util.DecodeHexAddress(req.Id)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid hex address %s, %s", req.Id, err.Error())
	}

	treeState, err := q.k.treeStates.Get(ctx, ismId.GetInternalId())
	if errors.Is(err, collections.ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "merkle tree state not found: %s", req.Id)
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryMerkleTreeStateResponse{
		TreeState: treeState,
	}, nil
}

// RootHistory implements types.QueryServer.
func (q queryServer) RootHistory(ctx context.Context, req *types.QueryRootHistoryRequest) (*types.QueryRootHistoryResponse, error) {
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

	stateRoots, err := historyEntries(ctx, q.k.stateRootHistory, ismId.GetInternalId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	treeStates, err := historyEntries(ctx, q.k.treeStateHistory, ismId.GetInternalId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryRootHistoryResponse{
		RootHistorySize: types.EffectiveRootHistorySize(ism.RootHistorySize),
		StateRoots:      stateRoots,
		TreeStates:      treeStates,
	}, nil
}
