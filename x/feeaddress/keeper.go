// Package feeaddress provides functionality for forwarding TIA tokens to the fee collector.
// Tokens sent to the fee address are automatically forwarded to validators via protocol-injected
// transactions in PrepareProposal.
package feeaddress

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v7/app/ante"
	"github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ types.MsgServer   = Keeper{}
	_ types.QueryServer = Keeper{}
)

// Keeper handles fee forwarding operations for the feeaddress module.
// Note: The actual fee transfer is done by FeeForwardDecorator in the ante handler.
// This keeper is responsible for the message handler (emitting events) and queries.
type Keeper struct{}

// NewKeeper creates a new Keeper instance.
func NewKeeper() Keeper {
	return Keeper{}
}

// ForwardFees handles MsgForwardFees by emitting the fee forwarded event.
// Note: The actual fee deduction is done by the FeeForwardDecorator in the ante handler.
// This message handler just emits the event for tracking purposes.
func (k Keeper) ForwardFees(ctx context.Context, _ *types.MsgForwardFees) (*types.MsgForwardFeesResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get the fee amount from context (set by FeeForwardDecorator)
	fee, ok := ante.GetFeeForwardAmount(sdkCtx)
	if !ok {
		// This shouldn't happen in normal operation as the ante decorator always sets the fee
		return nil, types.ErrFeeForwardAmountNotFound
	}

	// Emit the event for tracking
	if err := sdkCtx.EventManager().EmitTypedEvent(types.NewFeeForwardedEvent(types.FeeAddressBech32, fee.String())); err != nil {
		return nil, fmt.Errorf("failed to emit fee forwarded event: %w", err)
	}

	return &types.MsgForwardFeesResponse{}, nil
}

// FeeAddress implements the Query/FeeAddress gRPC method.
// Returns the address where tokens should be sent to be forwarded to validators.
func (k Keeper) FeeAddress(_ context.Context, _ *types.QueryFeeAddressRequest) (*types.QueryFeeAddressResponse, error) {
	return &types.QueryFeeAddressResponse{
		FeeAddress: types.FeeAddressBech32,
	}, nil
}
