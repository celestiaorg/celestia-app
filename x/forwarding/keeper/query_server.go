package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	k Keeper
}

// NewQueryServerImpl returns an implementation of the QueryServer interface
func NewQueryServerImpl(keeper Keeper) types.QueryServer {
	return &queryServer{k: keeper}
}

// DeriveForwardingAddress derives the forwarding address for given parameters
func (q queryServer) DeriveForwardingAddress(ctx context.Context, req *types.QueryDeriveForwardingAddressRequest) (*types.QueryDeriveForwardingAddressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	destRecipient, err := util.DecodeHexAddress(req.DestRecipient)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid dest_recipient hex %q: %v", req.DestRecipient, err)
	}

	if len(destRecipient.Bytes()) != types.RecipientLength {
		return nil, status.Errorf(codes.InvalidArgument, "dest_recipient must be %d bytes, got %d", types.RecipientLength, len(destRecipient.Bytes()))
	}

	// Derive the forwarding address
	forwardAddr := types.DeriveForwardingAddress(req.DestDomain, destRecipient.Bytes())

	return &types.QueryDeriveForwardingAddressResponse{
		Address: forwardAddr.String(),
	}, nil
}

// Params returns the module parameters
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params, err := q.k.GetParams(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			params = types.DefaultParams()
		} else {
			return nil, fmt.Errorf("failed to query params: %w", err)
		}
	}

	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}
