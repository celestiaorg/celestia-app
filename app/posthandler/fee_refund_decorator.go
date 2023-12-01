package posthandler

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
)

const (
	EventType                  = "fee_refund"
	AttributeKeyRefund         = "refund"
	AttributeKeyRefundReceiver = "refund_receiver"
)

// FeeRefundDecorator handles refunding a portion of the fee that was originally
// deducted from the feepayer but was not needed because the tx consumed less
// gas than the gas limit. CONTRACT: Tx must implement FeeTx interface to use
// FeeRefundDecorator
type FeeRefundDecorator struct {
	accountKeeper  authkeeper.AccountKeeper
	bankKeeper     types.BankKeeper
	feegrantKeeper feegrantkeeper.Keeper
}

func NewFeeRefundDecorator(ak authkeeper.AccountKeeper, bk types.BankKeeper, fk feegrantkeeper.Keeper) FeeRefundDecorator {
	return FeeRefundDecorator{
		accountKeeper:  ak,
		bankKeeper:     bk,
		feegrantKeeper: fk,
	}
}

func (frd FeeRefundDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	if !simulate && ctx.BlockHeight() > 0 && feeTx.GetGas() == 0 {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidGasLimit, "must provide positive gas")
	}

	fee := feeTx.GetFee()
	if err := frd.checkRefundFee(ctx, tx, fee); err != nil {
		return ctx, err
	}

	return next(ctx, tx, simulate)
}

func (dfd FeeRefundDecorator) checkRefundFee(ctx sdk.Context, sdkTx sdk.Tx, amountToRefund sdk.Coins) error {
	feeTx, ok := sdkTx.(sdk.FeeTx)
	if !ok {
		return sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	if addr := dfd.accountKeeper.GetModuleAddress(types.FeeCollectorName); addr == nil {
		return fmt.Errorf("fee collector module account (%s) has not been set", types.FeeCollectorName)
	}

	refundReceiver := getRefundReceiver(feeTx)
	refundReceiverAccount := dfd.accountKeeper.GetAccount(ctx, refundReceiver)
	if refundReceiverAccount == nil {
		return sdkerrors.ErrUnknownAddress.Wrapf("refund receiver address: %s does not exist", refundReceiverAccount)
	}

	if !amountToRefund.IsZero() {
		err := refund(dfd.bankKeeper, ctx, refundReceiverAccount, amountToRefund)
		if err != nil {
			return err
		}
	}

	events := sdk.Events{
		sdk.NewEvent(
			EventType,
			sdk.NewAttribute(AttributeKeyRefund, amountToRefund.String()),
			sdk.NewAttribute(AttributeKeyRefundReceiver, refundReceiverAccount.String()),
		),
	}
	ctx.EventManager().EmitEvents(events)

	return nil
}

func getRefundReceiver(feeTx sdk.FeeTx) sdk.AccAddress {
	if feeGranter := feeTx.FeeGranter(); feeGranter != nil {
		return feeGranter
	}
	return feeTx.FeePayer()
}

// refund sends amountToRefund from the fee collector module account to the refund receiver.
func refund(bankKeeper types.BankKeeper, ctx sdk.Context, refundReceiver types.AccountI, amountToRefund sdk.Coins) error {
	if !amountToRefund.IsValid() {
		return sdkerrors.Wrapf(sdkerrors.ErrInsufficientFee, "invalid fee amount: %s", amountToRefund)
	}

	err := bankKeeper.SendCoinsFromAccountToModule(ctx, refundReceiver.GetAddress(), types.FeeCollectorName, amountToRefund)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInsufficientFunds, err.Error())
	}

	return nil
}
