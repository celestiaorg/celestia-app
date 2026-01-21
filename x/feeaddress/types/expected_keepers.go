package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// FeeForwardBankKeeper defines the bank keeper interface needed by FeeForwardDecorator.
type FeeForwardBankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}
