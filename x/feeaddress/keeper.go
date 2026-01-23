// Package feeaddress provides functionality for forwarding TIA tokens to the fee collector.
// Tokens sent to the fee address are automatically forwarded to validators via protocol-injected
// transactions in PrepareProposal.
package feeaddress

import (
	"context"

	"github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
)

var _ types.MsgServer = Keeper{}

// Keeper handles fee forwarding operations for the feeaddress module.
// The keeper is intentionally stateless - the fee transfer happens in
// ProtocolFeeTerminatorDecorator (ante handler).
type Keeper struct{}

// NewKeeper creates a new Keeper instance.
func NewKeeper() Keeper {
	return Keeper{}
}

// PayProtocolFee handles MsgPayProtocolFee. The actual fee transfer is handled by
// ProtocolFeeTerminatorDecorator in the ante chain.
func (k Keeper) PayProtocolFee(_ context.Context, _ *types.MsgPayProtocolFee) (*types.MsgPayProtocolFeeResponse, error) {
	return &types.MsgPayProtocolFeeResponse{}, nil
}
