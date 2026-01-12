package keeper

import (
	"context"

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

// DeriveForwardingAddress derives the forwarding address for given parameters
func (q queryServer) DeriveForwardingAddress(ctx context.Context, req *types.QueryDeriveForwardingAddressRequest) (*types.QueryDeriveForwardingAddressResponse, error) {
	if req == nil {
		return nil, types.ErrAddressMismatch
	}

	// Parse destination recipient
	destRecipient, err := util.DecodeHexAddress(req.DestRecipient)
	if err != nil {
		return nil, err
	}

	// Verify it's 32 bytes
	if len(destRecipient.Bytes()) != 32 {
		return nil, types.ErrAddressMismatch
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
		// Return default params if not set
		params = types.DefaultParams()
	}

	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}
