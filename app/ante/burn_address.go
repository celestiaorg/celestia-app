package ante

import (
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	burntypes "github.com/celestiaorg/celestia-app/v7/x/burn/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

var _ sdk.AnteDecorator = BurnAddressDecorator{}

// BurnAddressDecorator rejects transactions that send non-utia tokens to the burn address.
type BurnAddressDecorator struct{}

func NewBurnAddressDecorator() *BurnAddressDecorator {
	return &BurnAddressDecorator{}
}

func (bad BurnAddressDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {
		case *banktypes.MsgSend:
			if err := validateBurnAddressSend(m.ToAddress, m.Amount); err != nil {
				return ctx, err
			}
		case *banktypes.MsgMultiSend:
			for _, output := range m.Outputs {
				if err := validateBurnAddressSend(output.Address, output.Coins); err != nil {
					return ctx, err
				}
			}
		case *ibctransfertypes.MsgTransfer:
			if err := validateBurnAddressSend(m.Receiver, sdk.NewCoins(m.Token)); err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}

// validateBurnAddressSend checks if the recipient is the burn address and
// ensures only utia is being sent.
func validateBurnAddressSend(recipient string, coins sdk.Coins) error {
	if recipient != burntypes.BurnAddressBech32 {
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
