// Package burn provides functionality for permanently destroying TIA tokens.
// It implements MsgBurn which allows users to burn utia from their accounts,
// reducing the total token supply.
package burn

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v6/x/burn/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper handles burn operations for the burn module.
type Keeper struct {
	bankKeeper types.BankKeeper
}

// NewKeeper creates a new Keeper instance with the provided BankKeeper.
func NewKeeper(bankKeeper types.BankKeeper) Keeper {
	return Keeper{bankKeeper: bankKeeper}
}

// Burn processes a MsgBurn request by transferring tokens from the signer's
// account to the burn module account and then permanently destroying them.
// It emits a typed EventBurn upon success.
func (k Keeper) Burn(goCtx context.Context, msg *types.MsgBurn) (*types.MsgBurnResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	signer, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("invalid signer address: %w", err)
	}

	coins := sdk.NewCoins(msg.Amount)

	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, signer, types.ModuleName, coins); err != nil {
		return nil, fmt.Errorf("failed to transfer to burn module: %w", err)
	}

	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		return nil, fmt.Errorf("failed to burn coins: %w", err)
	}

	if err := ctx.EventManager().EmitTypedEvent(types.NewBurnEvent(msg.Signer, msg.Amount.String())); err != nil {
		return nil, fmt.Errorf("failed to emit burn event: %w", err)
	}

	return &types.MsgBurnResponse{Burned: msg.Amount}, nil
}
