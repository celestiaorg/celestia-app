package ante

import (
	"context"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// FeeForwardBankKeeper defines the bank keeper interface needed by FeeForwardDecorator.
// Defined here (not in x/feeaddress/types) since it's only used by this ante decorator.
type FeeForwardBankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

// FeeForwardContextKey indicates a transaction is a protocol-injected fee forward transaction.
// Set by EarlyFeeForwardDetector BEFORE ValidateBasic runs, enabling SkipForFeeForwardDecorator
// to skip signature-related decorators for this unsigned transaction.
type FeeForwardContextKey struct{}

// FeeForwardAmountContextKey stores the fee amount deducted from the fee address.
// Set by FeeForwardDecorator AFTER the bank transfer completes. The keeper's
// ForwardFees message handler reads this to emit EventFeeForwarded.
type FeeForwardAmountContextKey struct{}

// FeeForwardDecorator handles MsgForwardFees transactions by rejecting user submissions
// in CheckTx, deducting the fee from the fee address, and sending it to the fee collector.
// Must be placed before DeductFeeDecorator, SetPubKeyDecorator, SigVerificationDecorator,
// and IncrementSequenceDecorator since fee forward txs have no signers.
type FeeForwardDecorator struct {
	bankKeeper FeeForwardBankKeeper
}

// NewFeeForwardDecorator creates a new FeeForwardDecorator.
func NewFeeForwardDecorator(bankKeeper FeeForwardBankKeeper) *FeeForwardDecorator {
	return &FeeForwardDecorator{
		bankKeeper: bankKeeper,
	}
}

// AnteHandle implements sdk.AnteDecorator.
func (d FeeForwardDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := feeaddresstypes.IsFeeForwardMsg(tx)
	if msg == nil {
		return next(ctx, tx, simulate)
	}

	// MsgForwardFees MUST NOT be submitted by users directly (CIP-43).
	// It is only valid when injected by the block proposer in PrepareProposal.
	if ctx.IsCheckTx() || ctx.IsReCheckTx() {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "MsgForwardFees cannot be submitted by users; it is protocol-injected only")
	}

	// Verify the context flag was set by EarlyFeeForwardDetector (defense-in-depth).
	if !IsFeeForwardTx(ctx) {
		return ctx, errors.Wrap(sdkerrors.ErrLogic, "fee forward context flag not set; EarlyFeeForwardDetector missing from ante chain")
	}

	// Get the fee from the transaction
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "tx must implement FeeTx")
	}
	fee := feeTx.GetFee()

	// Fee must be exactly one positive coin in the native denom (defense-in-depth, also checked in ProcessProposal)
	if len(fee) != 1 || fee[0].Denom != appconsts.BondDenom || !fee[0].Amount.IsPositive() {
		return ctx, errors.Wrapf(sdkerrors.ErrInvalidRequest, "fee forward tx requires exactly one positive %s coin, got %s", appconsts.BondDenom, fee)
	}

	// Deduct fee from fee address and send to fee collector
	err := d.bankKeeper.SendCoinsFromAccountToModule(ctx, feeaddresstypes.FeeAddress, authtypes.FeeCollectorName, fee)
	if err != nil {
		return ctx, errors.Wrap(err, "failed to deduct fee from fee address")
	}

	// Store the fee amount in context for the message handler to emit the event
	ctx = ctx.WithValue(FeeForwardAmountContextKey{}, fee)

	return next(ctx, tx, simulate)
}

// IsFeeForwardTx returns true if the context indicates this is a fee forward transaction.
func IsFeeForwardTx(ctx sdk.Context) bool {
	val := ctx.Value(FeeForwardContextKey{})
	if val == nil {
		return false
	}
	isFeeForward, ok := val.(bool)
	return ok && isFeeForward
}

// GetFeeForwardAmount returns the fee amount that was forwarded, if available in context.
func GetFeeForwardAmount(ctx sdk.Context) (sdk.Coins, bool) {
	val := ctx.Value(FeeForwardAmountContextKey{})
	if val == nil {
		return nil, false
	}
	fee, ok := val.(sdk.Coins)
	return fee, ok
}
