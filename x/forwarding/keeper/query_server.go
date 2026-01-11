package keeper

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
)

// QueryServer defines the query server for the forwarding module
type QueryServer interface {
	DeriveForwardingAddress(context.Context, *QueryDeriveForwardingAddressRequest) (*QueryDeriveForwardingAddressResponse, error)
	Params(context.Context, *QueryParamsRequest) (*QueryParamsResponse, error)
}

// QueryDeriveForwardingAddressRequest is the request type for DeriveForwardingAddress query
type QueryDeriveForwardingAddressRequest struct {
	DestDomain    uint32 `json:"dest_domain"`
	DestRecipient string `json:"dest_recipient"` // hex-encoded 32 bytes
}

// QueryDeriveForwardingAddressResponse is the response type for DeriveForwardingAddress query
type QueryDeriveForwardingAddressResponse struct {
	Address string `json:"address"` // bech32 address
}

// QueryParamsRequest is the request type for Params query
type QueryParamsRequest struct{}

// QueryParamsResponse is the response type for Params query
type QueryParamsResponse struct {
	Params types.Params `json:"params"`
}

var _ QueryServer = queryServer{}

type queryServer struct {
	k Keeper
}

// NewQueryServerImpl returns an implementation of the QueryServer interface
func NewQueryServerImpl(keeper Keeper) QueryServer {
	return &queryServer{k: keeper}
}

// DeriveForwardingAddress derives the forwarding address for given parameters
func (q queryServer) DeriveForwardingAddress(ctx context.Context, req *QueryDeriveForwardingAddressRequest) (*QueryDeriveForwardingAddressResponse, error) {
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

	return &QueryDeriveForwardingAddressResponse{
		Address: forwardAddr.String(),
	}, nil
}

// Params returns the module parameters
func (q queryServer) Params(ctx context.Context, req *QueryParamsRequest) (*QueryParamsResponse, error) {
	params, err := q.k.GetParams(ctx)
	if err != nil {
		// Return default params if not set
		params = types.DefaultParams()
	}

	return &QueryParamsResponse{
		Params: params,
	}, nil
}
