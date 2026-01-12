package burn

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/x/burn/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper defines the expected bank keeper interface
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

// Keeper handles burn operations
type Keeper struct {
	bankKeeper BankKeeper
}

// NewKeeper creates a new burn Keeper
func NewKeeper(bankKeeper BankKeeper) Keeper {
	return Keeper{bankKeeper: bankKeeper}
}

// Burn implements types.MsgServer
func (k Keeper) Burn(goCtx context.Context, msg *types.MsgBurn) (*types.MsgBurnResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	signer, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("invalid signer address: %w", err)
	}

	// Validate TIA-only
	if msg.Amount.Denom != appconsts.BondDenom {
		return nil, fmt.Errorf("only %s can be burned, got %s", appconsts.BondDenom, msg.Amount.Denom)
	}

	// Validate positive amount
	if !msg.Amount.IsPositive() {
		return nil, fmt.Errorf("burn amount must be positive")
	}

	coins := sdk.NewCoins(msg.Amount)

	// Transfer to module account (validates balance internally)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, signer, types.ModuleName, coins); err != nil {
		return nil, fmt.Errorf("failed to transfer to burn module: %w", err)
	}

	// Burn from module account
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		return nil, fmt.Errorf("failed to burn coins: %w", err)
	}

	// Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"burn",
			sdk.NewAttribute("burner", msg.Signer),
			sdk.NewAttribute("amount", msg.Amount.String()),
		),
	)

	return &types.MsgBurnResponse{Burned: msg.Amount}, nil
}
