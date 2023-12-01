package posthandler

import (
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
)

const (
	eventType                   = "fee_refund"
	attributeKeyRefund          = "refund"
	attributeKeyRefundRecipient = "refund_recipient"
	bondDenom                   = "utia"
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
	if err := frd.maybeRefund(ctx, tx); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

func (frd FeeRefundDecorator) maybeRefund(ctx sdk.Context, tx sdk.Tx) error {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return errors.Wrap(sdkerrors.ErrTxDecode, "tx must be a FeeTx")
	}

	coinsToRefund := getCoinsToRefund(ctx, feeTx)
	refundRecipient := getRefundRecipient(feeTx)
	refundRecipientAccount := frd.accountKeeper.GetAccount(ctx, refundRecipient)
	if refundRecipientAccount == nil {
		return errors.Wrapf(sdkerrors.ErrUnknownAddress, "refund recipient address: %s does not exist", refundRecipientAccount)
	}

	if err := frd.processRefund(frd.bankKeeper, ctx, refundRecipientAccount, coinsToRefund); err != nil {
		return err
	}

	events := sdk.Events{newFeeRefundEvent(coinsToRefund, refundRecipient)}
	ctx.EventManager().EmitEvents(events)

	return nil
}

func getCoinsToRefund(ctx sdk.Context, feeTx sdk.FeeTx) sdk.Coins {
	gasConsumed := ctx.GasMeter().GasConsumed()
	gasPrice := getGasPrice(feeTx)
	feeBasedOnGasConsumption := gasPrice.Amount.MulInt64(int64(gasConsumed)).Ceil().TruncateInt()
	amountToRefund := feeTx.GetFee().AmountOf(bondDenom).Sub(feeBasedOnGasConsumption)
	coinsToRefund := sdk.NewCoins(sdk.NewCoin(bondDenom, amountToRefund))
	return coinsToRefund
}

// processRefund sends amountToRefund from the fee collector module account to the refundRecipient.
func (frd FeeRefundDecorator) processRefund(bankKeeper types.BankKeeper, ctx sdk.Context, refundRecipient types.AccountI, amountToRefund sdk.Coins) error {
	to := refundRecipient.GetAddress()
	from := frd.accountKeeper.GetModuleAddress(types.FeeCollectorName)
	if from == nil {
		return fmt.Errorf("fee collector module account (%s) has not been set", types.FeeCollectorName)
	}

	if !amountToRefund.IsValid() {
		return fmt.Errorf("invalid amount to refund: %s", amountToRefund)
	}

	if err := bankKeeper.SendCoins(ctx, from, to, amountToRefund); err != nil {
		return errors.Wrapf(err, "error refunding %s from fee collector module account to %s", amountToRefund, to)
	}

	return nil
}

func getRefundRecipient(feeTx sdk.FeeTx) sdk.AccAddress {
	if feeGranter := feeTx.FeeGranter(); feeGranter != nil {
		return feeGranter
	}
	return feeTx.FeePayer()
}

func getGasPrice(feeTx sdk.FeeTx) sdk.DecCoin {
	feeCoins := feeTx.GetFee()
	gas := feeTx.GetGas()
	// gas * gasPrice = fees. So gasPrice = fees / gas.
	gasPrice := sdk.NewDecFromInt(feeCoins.AmountOf(bondDenom)).Quo(sdk.NewDec(int64(gas)))
	return sdk.NewDecCoinFromDec(bondDenom, gasPrice)
}

func newFeeRefundEvent(amountToRefund sdk.Coins, refundRecipient sdk.AccAddress) sdk.Event {
	return sdk.NewEvent(
		eventType,
		sdk.NewAttribute(attributeKeyRefund, amountToRefund.String()),
		sdk.NewAttribute(attributeKeyRefundRecipient, refundRecipient.String()),
	)
}
