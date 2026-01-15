package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
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

	// Check if there's a warp route to the destination domain
	// We check using the TIA collateral token since that's the primary use case
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := q.k.GetParams(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, status.Errorf(codes.Internal, "failed to get params: %v", err)
	}

	// Only validate route if TiaCollateralTokenId is configured
	if params.TiaCollateralTokenId != "" {
		tokenId, err := util.DecodeHexAddress(params.TiaCollateralTokenId)
		if err != nil {
			sdkCtx.Logger().Warn("invalid TiaCollateralTokenId in params", "error", err)
		} else {
			hasRoute, err := q.k.HasEnrolledRouter(ctx, tokenId, req.DestDomain)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to check warp route: %v", err)
			}
			if !hasRoute {
				return nil, status.Errorf(codes.FailedPrecondition, "%s: domain %d has no configured warp route", types.ErrNoWarpRoute.Error(), req.DestDomain)
			}
		}
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
