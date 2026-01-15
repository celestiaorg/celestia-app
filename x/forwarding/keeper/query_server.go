package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

// DeriveForwardingAddress derives the forwarding address for given parameters.
// Returns an error if the destination domain has no warp route configured.
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

	// Check if there's a TIA warp route to the destination domain
	_, err = q.k.findTIACollateralTokenForDomain(ctx, req.DestDomain)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "no warp route to domain %d: %v", req.DestDomain, err)
	}

	// Derive the forwarding address
	forwardAddr, err := types.DeriveForwardingAddress(req.DestDomain, destRecipient.Bytes())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to derive address: %v", err)
	}

	return &types.QueryDeriveForwardingAddressResponse{
		Address: sdk.AccAddress(forwardAddr).String(),
	}, nil
}

// Params returns the module parameters
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}

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
