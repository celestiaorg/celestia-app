package keeper

import (
	"context"
	"fmt"

	"github.com/lazyledger/lazyledger-core/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/lazyledger/lazyledger-app/x/lazyledgerapp/types"
)

// todo(evan): move these somewhere else
const (
	TokenDenomination = "token"
)

type Keeper struct {
	cdc      codec.Marshaler
	storeKey sdk.StoreKey
	memKey   sdk.StoreKey
	bank     BankKeeper
	baseFee  sdk.Int
}

func NewKeeper(cdc codec.Marshaler, bank BankKeeper, storeKey, memKey sdk.StoreKey, baseFee sdk.Int) *Keeper {
	return &Keeper{
		cdc:      cdc,
		storeKey: storeKey,
		memKey:   memKey,
		bank:     bank,
		baseFee:  baseFee,
	}
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// PayForMessage moves a user's coins to the module address and burns them.
func (k Keeper) PayForMessage(goCtx context.Context, msg *types.MsgWirePayForMessage) (*types.MsgPayForMessageResponse, error) {
	// don't pay for fees for the first version
	return &types.MsgPayForMessageResponse{}, nil
}

// BankKeeper restricts the funtionality of the bank keeper used in the lazyledgerapp keeper
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	BurnCoins(ctx sdk.Context, moduleName string, amt sdk.Coins) error
}
