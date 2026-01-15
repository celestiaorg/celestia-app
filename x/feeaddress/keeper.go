// Package feeaddress provides functionality for forwarding TIA tokens to the fee collector.
// Tokens sent to the fee address are automatically forwarded to validators at the end of each block.
package feeaddress

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// Keeper handles fee forwarding operations for the feeaddress module.
type Keeper struct {
	bankKeeper types.BankKeeper
}

// NewKeeper creates a new Keeper instance.
func NewKeeper(bankKeeper types.BankKeeper) Keeper {
	return Keeper{
		bankKeeper: bankKeeper,
	}
}

// EndBlocker forwards any utia tokens at the fee address to the fee collector.
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	balance := k.bankKeeper.GetBalance(sdkCtx, types.FeeAddress, appconsts.BondDenom)
	if balance.IsZero() {
		return nil
	}

	coins := sdk.NewCoins(balance)

	// Forward to fee collector for distribution to validators
	if err := k.bankKeeper.SendCoinsFromAccountToModule(sdkCtx, types.FeeAddress, authtypes.FeeCollectorName, coins); err != nil {
		return fmt.Errorf("failed to forward to fee collector: %w", err)
	}

	if err := sdkCtx.EventManager().EmitTypedEvent(types.NewFeeForwardedEvent(types.FeeAddressBech32, balance.String())); err != nil {
		return fmt.Errorf("failed to emit fee forwarded event: %w", err)
	}

	return nil
}

// FeeAddress implements the Query/FeeAddress gRPC method.
// Returns the address where tokens should be sent to be forwarded to validators.
func (k Keeper) FeeAddress(_ context.Context, _ *types.QueryFeeAddressRequest) (*types.QueryFeeAddressResponse, error) {
	return &types.QueryFeeAddressResponse{
		FeeAddress: types.FeeAddressBech32,
	}, nil
}
