package ante

import (
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	burntypes "github.com/celestiaorg/celestia-app/v7/x/burn/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

var _ sdk.AnteDecorator = BurnAddressDecorator{}

// BurnAddressDecorator rejects transactions that send non-utia tokens to the burn address.
// This includes messages nested inside authz.MsgExec.
//
// Note: ICA host executed messages bypass ante handlers. If ICA sends non-utia to the
// burn address, the tokens would be permanently stuck (not burned, not stolen).
type BurnAddressDecorator struct{}

func NewBurnAddressDecorator() *BurnAddressDecorator {
	return &BurnAddressDecorator{}
}

func (bad BurnAddressDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	for _, msg := range tx.GetMsgs() {
		if err := bad.validateMessage(msg); err != nil {
			return ctx, err
		}
	}
	return next(ctx, tx, simulate)
}

// validateMessage checks a message and any nested messages for burn address violations.
func (bad BurnAddressDecorator) validateMessage(msg sdk.Msg) error {
	switch m := msg.(type) {
	case *banktypes.MsgSend:
		if err := validateBurnAddressSend(m.ToAddress, m.Amount); err != nil {
			return err
		}
	case *banktypes.MsgMultiSend:
		for _, output := range m.Outputs {
			if err := validateBurnAddressSend(output.Address, output.Coins); err != nil {
				return err
			}
		}
	case *ibctransfertypes.MsgTransfer:
		if err := validateBurnAddressSend(m.Receiver, sdk.NewCoins(m.Token)); err != nil {
			return err
		}
	case *authz.MsgExec:
		nestedMsgs, err := m.GetMessages()
		if err != nil {
			return err
		}
		for _, nestedMsg := range nestedMsgs {
			if err := bad.validateMessage(nestedMsg); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateBurnAddressSend checks if the recipient is the burn address and
// ensures only utia is being sent. Uses bytes comparison for safety.
func validateBurnAddressSend(recipient string, coins sdk.Coins) error {
	addr, err := sdk.AccAddressFromBech32(recipient)
	if err != nil {
		// Invalid address - let other validators handle this
		return nil
	}

	if !addr.Equals(burntypes.BurnAddress) {
		return nil
	}

	for _, coin := range coins {
		if coin.Denom != appconsts.BondDenom {
			return sdkerrors.ErrInvalidRequest.Wrapf(
				"only %s can be sent to burn address, got %s",
				appconsts.BondDenom,
				coin.Denom,
			)
		}
	}
	return nil
}
