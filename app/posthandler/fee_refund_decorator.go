package posthandler

import (
	"fmt"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
)

// UnspentGasRefundDecorator handles refunding a portion of the tx fee that was
// originally deducted from the feepayer but was not needed because the tx
// consumed less gas than the gas limit.
type UnspentGasRefundDecorator struct {
	accountKeeper  authkeeper.AccountKeeper
	bankKeeper     types.BankKeeper
	feegrantKeeper feegrantkeeper.Keeper
}

func NewUnspentGasRefundDecorator(ak authkeeper.AccountKeeper, bk types.BankKeeper, fk feegrantkeeper.Keeper) UnspentGasRefundDecorator {
	return UnspentGasRefundDecorator{
		accountKeeper:  ak,
		bankKeeper:     bk,
		feegrantKeeper: fk,
	}
}

func (frd UnspentGasRefundDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if err := frd.maybeRefund(ctx, tx); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

func (frd UnspentGasRefundDecorator) maybeRefund(ctx sdk.Context, tx sdk.Tx) error {
	// Replace the context's gas meter with an infinite gas meter so that this
	// decorator doesn't run out of gas while refunding.
	gasMeter := ctx.GasMeter()
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeter())

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return errors.Wrap(sdkerrors.ErrTxDecode, "tx must be a FeeTx to use FeeRefundDecorator")
	}

	if gasMeter.IsOutOfGas() {
		// If the gas meter is out of gas, then no refund needs to be issued.
		return nil
	}

	coinsToRefund := getCoinsToRefund(gasMeter, feeTx)
	refundRecipient := getRefundRecipient(feeTx)
	refundRecipientAccount := frd.accountKeeper.GetAccount(ctx, refundRecipient)
	if refundRecipientAccount == nil {
		return errors.Wrapf(sdkerrors.ErrUnknownAddress, "refund recipient address: %s does not exist", refundRecipientAccount)
	}

	if err := frd.processRefund(frd.bankKeeper, ctx, refundRecipientAccount, coinsToRefund); err != nil {
		return err
	}

	return nil
}

func getCoinsToRefund(gasMeter sdk.GasMeter, feeTx sdk.FeeTx) sdk.Coins {
	gasConsumed := gasMeter.GasConsumed()
	gasPrice := getGasPrice(feeTx)
	feeBasedOnGasConsumption := gasPrice.Amount.MulInt64(int64(gasConsumed)).Ceil().TruncateInt()
	amountToRefund := feeTx.GetFee().AmountOf(appconsts.BondDenom).Sub(feeBasedOnGasConsumption)
	coinsToRefund := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, amountToRefund))
	return coinsToRefund
}

// processRefund sends amountToRefund from the fee collector module account to the refundRecipient.
func (frd UnspentGasRefundDecorator) processRefund(bankKeeper types.BankKeeper, ctx sdk.Context, refundRecipient types.AccountI, amountToRefund sdk.Coins) error {
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
	gasPrice := sdk.NewDecFromInt(feeCoins.AmountOf(appconsts.BondDenom)).Quo(sdk.NewDec(int64(gas)))
	return sdk.NewDecCoinFromDec(appconsts.BondDenom, gasPrice)
}
