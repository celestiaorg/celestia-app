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
//
// Design note: The keeper is intentionally stateless and has no dependencies.
// The fee transfer happens in the ante handler (FeeForwardDecorator), not here.
// This separation exists because:
//  1. The ante handler runs BEFORE message execution - it deducts the fee from
//     the fee address and sends it to the fee collector as a real tx fee.
//  2. The message handler (ForwardFees) runs AFTER the ante handler completes.
//     It reads the fee amount from context (set by ante handler) to emit an event.
//
// This context-based coupling between ante handler and message handler is intentional:
// - Events should be emitted during message execution (standard SDK pattern)
// - But the fee transfer must happen during ante handling (before execution)
// - So we pass the fee amount via context from ante to message handler
//
// Alternative considered: Emit event in ante handler directly. Rejected because
// events in ante handlers are less discoverable and don't follow SDK conventions.
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
