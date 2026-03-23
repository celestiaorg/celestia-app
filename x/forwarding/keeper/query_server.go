package keeper

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
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

	tokenID, err := util.DecodeHexAddress(req.TokenId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid token_id hex %q: %v", req.TokenId, err)
	}

	token, err := q.k.warpKeeper.GetHypToken(ctx, tokenID.GetInternalId())
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "token %s not found: %v", req.TokenId, err)
	}

	hasRoute, err := q.k.HasEnrolledRouter(ctx, token.Id, req.DestDomain)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check route: %v", err)
	}
	if !hasRoute {
		return nil, status.Errorf(codes.FailedPrecondition, "no warp route for token %s to domain %d", req.TokenId, req.DestDomain)
	}

	forwardAddr, err := types.DeriveForwardingAddress(req.DestDomain, destRecipient.Bytes(), tokenID.Bytes())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to derive address: %v", err)
	}

	return &types.QueryDeriveForwardingAddressResponse{
		Address: sdk.AccAddress(forwardAddr).String(),
	}, nil
}

// QuoteForwardingFee returns the estimated IGP fee for forwarding a specific token to a destination domain.
// Relayers should query this before submitting MsgForward to determine the required max_igp_fee.
func (q queryServer) QuoteForwardingFee(ctx context.Context, req *types.QueryQuoteForwardingFeeRequest) (*types.QueryQuoteForwardingFeeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	tokenID, err := util.DecodeHexAddress(req.TokenId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid token_id hex %q: %v", req.TokenId, err)
	}

	token, err := q.k.warpKeeper.GetHypToken(ctx, tokenID.GetInternalId())
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "token %s not found: %v", req.TokenId, err)
	}

	hasRoute, err := q.k.HasEnrolledRouter(ctx, token.Id, req.DestDomain)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check route: %v", err)
	}
	if !hasRoute {
		return nil, status.Errorf(codes.FailedPrecondition, "no warp route for token %s to domain %d", req.TokenId, req.DestDomain)
	}

	fee, err := q.k.QuoteIgpFeeForToken(sdk.UnwrapSDKContext(ctx), token, req.DestDomain)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "failed to quote IGP fee: %v", err)
	}

	return &types.QueryQuoteForwardingFeeResponse{
		Fee: fee,
	}, nil
}
