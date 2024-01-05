package posthandler

import (
	"fmt"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
)

// MaxRefundGasConsumed is the maximum amount of gasConsumed that can be refunded.
var MaxRefundGasConsumed = sdk.NewDecWithPrec(5, 1) // 50%

// RefundGasCost is the amount of gas consumed during the execution of this
// posthandler. If a tx reaches this posthandler with gas remaining in excess of
// this amount, then a refund will be issued for the gas remaining -
// RefundGasCost.
//
// NOTE: this value was determined empirically.
const RefundGasCost = 15_000

// RefundGasRemainingDecorator handles refunding a portion of the tx fee that
// was originally deducted from the feepayer (or feegranter) but was not needed
// because the tx consumed less gas than the gas limit.
type RefundGasRemainingDecorator struct {
	accountKeeper  authkeeper.AccountKeeper
	bankKeeper     authtypes.BankKeeper
	feegrantKeeper feegrantkeeper.Keeper
}

// NewRefundGasRemainingDecorator returns a new RefundGasRemainingDecorator.
func NewRefundGasRemainingDecorator(ak authkeeper.AccountKeeper, bk authtypes.BankKeeper, fk feegrantkeeper.Keeper) RefundGasRemainingDecorator {
	return RefundGasRemainingDecorator{
		accountKeeper:  ak,
		bankKeeper:     bk,
		feegrantKeeper: fk,
	}
}

// AnteHandle implements the cosmos-sdk AnteHandler interface. Note: the
// AnteHandler interface is also used for post-handlers.
func (d RefundGasRemainingDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if err := d.maybeRefund(ctx, tx, simulate); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

// maybeRefund conditionally refunds a portion of the tx fee to the fee payer.
func (d RefundGasRemainingDecorator) maybeRefund(ctx sdk.Context, tx sdk.Tx, simulate bool) error {
	// If this tx was included in a v1 block then no refund needs to be issued.
	// Gas refunds are only applicable for blocks >= v2.Version
	if ctx.BlockHeader().Version.App == v1.Version {
		return nil
	}

	// If this is a simulation then no state changes should be persisted and
	// therefore no refund needs to be issued.
	if simulate {
		return nil
	}

	// Replace the context's gas meter with an infinite gas meter so that this
	// posthandler doesn't run out of gas while refunding.
	gasMeter := ctx.GasMeter()
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeter())
	// Restore the original gas meter after this posthandler is done.
	defer ctx.WithGasMeter(gasMeter)

	// If the gas meter doesn't have enough gas remaining to cover the
	// refund gas cost, then no refund needs to be issued.
	if gasMeter.GasRemaining() < RefundGasCost {
		return nil
	}
	gasMeter.ConsumeGas(RefundGasCost, "refund gas cost")

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return errors.Wrap(sdkerrors.ErrTxDecode, "tx must be a FeeTx to use FeeRefundDecorator")
	}

	refund := getRefund(gasMeter, feeTx)
	if len(refund) == 0 {
		return nil
	}
	recipient := getRecipient(feeTx)

	if err := d.processRefund(ctx, refund, recipient); err != nil {
		return err
	}

	return nil
}

// processRefund sends amountToRefund from the fee collector module account to the recipient.
func (d RefundGasRemainingDecorator) processRefund(ctx sdk.Context, refund sdk.Coins, recipient sdk.AccAddress) error {
	from := d.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	if from == nil {
		return fmt.Errorf("fee collector module account (%s) has not been set", authtypes.FeeCollectorName)
	}

	if recipientAccount := d.accountKeeper.GetAccount(ctx, recipient); recipientAccount == nil {
		return errors.Wrapf(sdkerrors.ErrUnknownAddress, "recipient address: %s does not exist", recipient)
	}

	if !refund.IsValid() {
		return fmt.Errorf("invalid coins to refund: %s", refund)
	}

	if err := d.bankKeeper.SendCoins(ctx, from, recipient, refund); err != nil {
		return errors.Wrapf(err, "error refunding %s from fee collector module account to %s", refund, recipient)
	}

	return nil
}

// getRefund returns the coins that should be refunded to the recipient.
func getRefund(gasMeter sdk.GasMeter, feeTx sdk.FeeTx) sdk.Coins {
	gasPrice := getGasPrice(feeTx)
	toRefund := gasPrice.Amount.MulInt(sdk.NewIntFromUint64(gasMeter.GasRemaining())).TruncateInt()
	maxToRefund := MaxRefundGasConsumed.Mul(gasPrice.Amount.MulInt(sdk.NewIntFromUint64(gasMeter.GasConsumed()))).TruncateInt()
	amountToRefund := minimum(toRefund, maxToRefund)
	return sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, amountToRefund))
}

// getRecipient returns the address that should receive the refund.
func getRecipient(feeTx sdk.FeeTx) sdk.AccAddress {
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
	gasPrice := sdk.NewDecFromInt(feeCoins.AmountOf(appconsts.BondDenom)).Quo(sdk.NewDecFromInt(sdk.NewIntFromUint64(gas)))
	return sdk.NewDecCoinFromDec(appconsts.BondDenom, gasPrice)
}

// minimum returns the smaller of a and b.
func minimum(a, b math.Int) math.Int {
	if a.LTE(b) {
		return a
	}
	return b
}
