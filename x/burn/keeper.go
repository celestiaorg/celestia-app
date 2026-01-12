package burn

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v6/x/burn/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

type Keeper struct {
	bankKeeper BankKeeper
}

func NewKeeper(bankKeeper BankKeeper) Keeper {
	return Keeper{bankKeeper: bankKeeper}
}

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

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeBurn,
			sdk.NewAttribute(types.AttributeKeyBurner, msg.Signer),
			sdk.NewAttribute(types.AttributeKeyAmount, msg.Amount.String()),
		),
	)

	return &types.MsgBurnResponse{Burned: msg.Amount}, nil
}
