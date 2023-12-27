package posthandler

import (
	"fmt"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
)

// MaxPortionOfFeeToRefund is the maximum portion of the fee that can be refunded.
var MaxPortionOfFeeToRefund = sdk.NewDecWithPrec(5, 1) // 50%

// RefundGasCost is the amount of gas consumed during the execution of this
// posthandler. If a tx reaches this posthandler with gas remaining in excess of
// this amount, then a refund will be issued for the gas remaining -
// RefundGasCost.
//
// NOTE: this value was determined empirically.
const RefundGasCost = 15_000

// RefundGasRemainingDecorator handles refunding a portion of the tx fee that was
// originally deducted from the feepayer but was not needed because the tx
// consumed less gas than the gas limit.
type RefundGasRemainingDecorator struct {
	accountKeeper  authkeeper.AccountKeeper
	bankKeeper     types.BankKeeper
	feegrantKeeper feegrantkeeper.Keeper
}

// NewRefundGasRemainingDecorator returns a new RefundGasRemainingDecorator.
func NewRefundGasRemainingDecorator(ak authkeeper.AccountKeeper, bk types.BankKeeper, fk feegrantkeeper.Keeper) RefundGasRemainingDecorator {
	return RefundGasRemainingDecorator{
		accountKeeper:  ak,
		bankKeeper:     bk,
		feegrantKeeper: fk,
	}
}

// AnteHandle implements the cosmos-sdk AnteHandler interface. Note: the
// AnteHandler interface is also used for post-handlers.
func (rgrd RefundGasRemainingDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// TODO: How to handle the simulate flag?
	if err := rgrd.maybeRefund(ctx, tx, simulate); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

// maybeRefund conditionally refunds a portion of the tx fee to the fee payer.
func (rgrd RefundGasRemainingDecorator) maybeRefund(ctx sdk.Context, tx sdk.Tx, simulate bool) error {
	// TODO: How to handle the simulate flag?

	// Replace the context's gas meter with an infinite gas meter so that this
	// posthandler doesn't run out of gas while refunding.
	gasMeter := ctx.GasMeter()
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeter())

	if gasMeter.GasRemaining() < RefundGasCost {
		// If the gas meter doesn't have enough gas remaining to cover the
		// refund gas cost, then no refund needs to be issued.
		return nil
	}
	gasMeter.ConsumeGas(RefundGasCost, "refund gas cost")

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return errors.Wrap(sdkerrors.ErrTxDecode, "tx must be a FeeTx to use FeeRefundDecorator")
	}

	coinsToRefund := getCoinsToRefund(gasMeter, feeTx)
	recipient := getRefundRecipient(feeTx)

	if err := rgrd.processRefund(ctx, coinsToRefund, recipient); err != nil {
		return err
	}

	return nil
}

// getCoinsToRefund returns the amount of coins to refund to the recipient.
func getCoinsToRefund(gasMeter sdk.GasMeter, feeTx sdk.FeeTx) sdk.Coins {
	gasPrice := getGasPrice(feeTx)
	toRefund := gasPrice.Amount.MulInt64(int64(gasMeter.GasRemaining())).TruncateInt()
	maxToRefund := MaxPortionOfFeeToRefund.MulInt(feeTx.GetFee().AmountOf(appconsts.BondDenom)).TruncateInt()
	amountToRefund := minimum(toRefund, maxToRefund)
	coinsToRefund := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, amountToRefund))
	return coinsToRefund
}

// processRefund sends amountToRefund from the fee collector module account to the recipient.
func (frd RefundGasRemainingDecorator) processRefund(ctx sdk.Context, amountToRefund sdk.Coins, recipient sdk.AccAddress) error {
	from := frd.accountKeeper.GetModuleAddress(types.FeeCollectorName)
	if from == nil {
		return fmt.Errorf("fee collector module account (%s) has not been set", types.FeeCollectorName)
	}

	if recipientAccount := frd.accountKeeper.GetAccount(ctx, recipient); recipientAccount == nil {
		return errors.Wrapf(sdkerrors.ErrUnknownAddress, "recipient address: %s does not exist", recipientAccount)
	}

	if !amountToRefund.IsValid() {
		return fmt.Errorf("invalid amount to refund: %s", amountToRefund)
	}

	if err := frd.bankKeeper.SendCoins(ctx, from, recipient, amountToRefund); err != nil {
		return errors.Wrapf(err, "error refunding %s from fee collector module account to %s", amountToRefund, recipient)
	}

	return nil
}

// getRefundRecipient returns the address that should receive the refund.
func getRefundRecipient(feeTx sdk.FeeTx) sdk.AccAddress {
	if feeGranter := feeTx.FeeGranter(); feeGranter != nil {
		return feeGranter
	}
	return feeTx.FeePayer()
}

// getGasPrice returns the gas price of the feeTx.
// gasLimit * gasPrice = fee. So gasPrice = fee / gasLimit.
func getGasPrice(feeTx sdk.FeeTx) sdk.DecCoin {
	feeCoins := feeTx.GetFee()
	gas := feeTx.GetGas()
	gasPrice := sdk.NewDecFromInt(feeCoins.AmountOf(appconsts.BondDenom)).Quo(sdk.NewDec(int64(gas)))
	return sdk.NewDecCoinFromDec(appconsts.BondDenom, gasPrice)
}

// minimum returns the smaller of a and b.
func minimum(a, b math.Int) math.Int {
	if a.LTE(b) {
		return a
	}
	return b
}
