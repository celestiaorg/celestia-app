package feeaddress

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ProtocolFeeBankKeeper defines the bank keeper interface needed by ProtocolFeeTerminatorDecorator.
type ProtocolFeeBankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}
