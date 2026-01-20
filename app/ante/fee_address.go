package ante

import (
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

var _ sdk.AnteDecorator = FeeAddressDecorator{}

// FeeAddressDecorator rejects transactions that send non-utia tokens to the fee address.
// This includes messages nested inside authz.MsgExec.
//
// Note: ICA host executed messages bypass ante handlers. If ICA sends non-utia to the
// fee address, the tokens would be permanently stuck.
type FeeAddressDecorator struct{}

func NewFeeAddressDecorator() *FeeAddressDecorator {
	return &FeeAddressDecorator{}
}

func (fad FeeAddressDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	for _, msg := range tx.GetMsgs() {
		if err := fad.validateMessage(msg); err != nil {
			return ctx, err
		}
	}
	return next(ctx, tx, simulate)
}

// validateMessage checks a message and any nested messages for fee address violations.
func (fad FeeAddressDecorator) validateMessage(msg sdk.Msg) error {
	switch m := msg.(type) {
	case *banktypes.MsgSend:
		return validateFeeAddressSend(m.ToAddress, m.Amount)
	case *banktypes.MsgMultiSend:
		for _, output := range m.Outputs {
			if err := validateFeeAddressSend(output.Address, output.Coins); err != nil {
				return err
			}
		}
	case *ibctransfertypes.MsgTransfer:
		return validateFeeAddressSend(m.Receiver, sdk.NewCoins(m.Token))
	case *authz.MsgExec:
		nestedMsgs, err := m.GetMessages()
		if err != nil {
			return err
		}
		for _, nestedMsg := range nestedMsgs {
			if err := fad.validateMessage(nestedMsg); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateFeeAddressSend checks if the recipient is the fee address and ensures only utia is being sent.
func validateFeeAddressSend(recipient string, coins sdk.Coins) error {
	addr, err := sdk.AccAddressFromBech32(recipient)
	if err != nil {
		// Invalid address - let other validators handle this
		return nil
	}

	if !addr.Equals(feeaddresstypes.FeeAddress) {
		return nil
	}

	for _, coin := range coins {
		if coin.Denom != appconsts.BondDenom {
			return sdkerrors.ErrInvalidRequest.Wrapf(
				"only %s can be sent to fee address, got %s",
				appconsts.BondDenom,
				coin.Denom,
			)
		}
	}
	return nil
}
