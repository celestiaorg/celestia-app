// Package feeaddress provides functionality for forwarding TIA tokens to the fee collector.
// Tokens sent to the fee address are automatically forwarded to validators via protocol-injected
// transactions in PrepareProposal.
package feeaddress

import (
	"context"

	"github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
)

var (
	_ types.MsgServer   = Keeper{}
	_ types.QueryServer = Keeper{}
)

// Keeper handles fee forwarding operations for the feeaddress module.
// The keeper is intentionally stateless - the fee transfer and event emission
// happen in FeeForwardTerminatorDecorator (ante handler).
type Keeper struct{}

// NewKeeper creates a new Keeper instance.
func NewKeeper() Keeper {
	return Keeper{}
}

// ForwardFees handles MsgForwardFees. The actual fee transfer and event emission
// is handled by FeeForwardTerminatorDecorator in the ante chain.
func (k Keeper) ForwardFees(_ context.Context, _ *types.MsgForwardFees) (*types.MsgForwardFeesResponse, error) {
	return &types.MsgForwardFeesResponse{}, nil
}

// FeeAddress implements the Query/FeeAddress gRPC method.
// Returns the address where tokens should be sent to be forwarded to validators.
func (k Keeper) FeeAddress(_ context.Context, _ *types.QueryFeeAddressRequest) (*types.QueryFeeAddressResponse, error) {
	return &types.QueryFeeAddressResponse{
		FeeAddress: types.FeeAddressBech32,
	}, nil
}
