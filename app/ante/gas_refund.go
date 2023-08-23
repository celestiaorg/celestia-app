package ante

import (
	"fmt"

	appconsts "github.com/celestiaorg/celestia-app/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// GasRefundDecorator is an AnteDecorator that refunds gas to the fee payer / fee granter.
// It's important to keep this method in sync with the DeductFeeDecorator.
type GasRefundDecorator struct {
	version    uint64
	bankKeeper BankKeeper
}

// NewGasRefundDecorator creates a new GasRefundDecorator.
func NewGasRefundDecorator(
	version uint64,
	bankKeeper BankKeeper,
) GasRefundDecorator {
	return GasRefundDecorator{
		version:    version,
		bankKeeper: bankKeeper,
	}
}

// AnteHandle implements the AnteHandler interface. It returns the funds paid for any remaining gas after executing the
// transaction. This should be placed as the last post handler. It only works with fees paid in the native denomination.
// This assumes that the DeductFeeDecorator has already been run. Thus it does not need to check that the fee collector
// module account exists nor that the feegranter, if present, is valid.
func (d GasRefundDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// only called when actually executing the tranasaction
	if simulate || ctx.IsReCheckTx() || ctx.IsCheckTx() {
		return next(ctx, tx, simulate)
	}

	// only provide refunds on accepted versions
	if ctx.BlockHeader().Version.App < d.version {
		return next(ctx, tx, simulate)
	}

	// only fee paying transactions are supported
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return next(ctx, tx, simulate)
	}

	// only non zero TIA transactions are supported
	fee := feeTx.GetFee().AmountOf(appconsts.BondDenom)
	if fee.IsZero() {
		return next(ctx, tx, simulate)
	}

	// work out who paid for the transaction
	feePayer := feeTx.FeePayer()
	feeGranter := feeTx.FeeGranter()
	if feeGranter != nil {
		feePayer = feeGranter
	}

	gasAllocated := feeTx.GetGas()
	gasRemaining := ctx.GasMeter().GasRemaining()
	// avoid exceptions with zeros or other invariants
	if gasAllocated == 0 || gasRemaining == 0 || gasAllocated < gasRemaining {
		return next(ctx, tx, simulate)
	}

	// calculate the refund. Since we are working with ints, we first do the multiplication
	// part and then the division. Ints truncate so we naturally round down.
	refund := fee.Mul(sdk.NewIntFromUint64(gasRemaining)).Quo(sdk.NewIntFromUint64(gasAllocated))
	// perform the refund
	err := d.bankKeeper.SendCoinsFromModuleToAccount(ctx, authtypes.FeeCollectorName, feePayer, sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, refund)))
	if err != nil {
		return ctx, fmt.Errorf("refund failed: %w", err)
	}

	return next(ctx, tx, simulate)
}

type BankKeeper interface {
	SendCoinsFromModuleToAccount(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}
