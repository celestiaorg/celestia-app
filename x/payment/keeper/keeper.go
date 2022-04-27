package keeper

import (
	"context"
	"fmt"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper handles all the state changes for the celestia-app module.
type Keeper struct {
	cdc      codec.BinaryCodec
	storeKey sdk.StoreKey
	memKey   sdk.StoreKey
	bank     BankKeeper
}

func NewKeeper(cdc codec.BinaryCodec, bank BankKeeper, storeKey, memKey sdk.StoreKey) *Keeper {
	return &Keeper{
		cdc:      cdc,
		storeKey: storeKey,
		memKey:   memKey,
		bank:     bank,
	}
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

//  MsgPayForData moves a user's coins to the module address and burns them.
func (k Keeper) PayForData(goCtx context.Context, msg *types.MsgPayForData) (*types.MsgPayForDataResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	ctx.EventManager().EmitEvent(
		types.NewPayForDataEvent(sdk.AccAddress(msg.Signer).String(), msg.GetMessageSize()),
	)

	return &types.MsgPayForDataResponse{}, nil
}

// BankKeeper restricts the funtionality of the bank keeper used in the payment keeper
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	BurnCoins(ctx sdk.Context, moduleName string, amt sdk.Coins) error
}
