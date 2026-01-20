package ante

import (
	"context"
	"fmt"

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

// FeeForwardContextKey is a context key that indicates a transaction is a
// protocol-injected fee forward transaction. Set by EarlyFeeForwardDetector
// BEFORE ValidateBasic runs, so that SkipForFeeForwardDecorator can skip
// signature-related decorators for this unsigned transaction.
//
// Note: We need two separate context keys because:
// - FeeForwardContextKey must be set EARLY (before ValidateBasic) to skip decorators
// - FeeForwardAmountContextKey is set LATER (after bank transfer) with the actual amount
// Consolidating them would require the amount to be known before the transfer completes.
type FeeForwardContextKey struct{}

// FeeForwardAmountContextKey stores the fee amount deducted from the fee address.
// Set by FeeForwardDecorator AFTER the bank transfer completes. The keeper's
// ForwardFees message handler reads this to emit EventFeeForwarded.
type FeeForwardAmountContextKey struct{}

// FeeForwardDecorator handles MsgForwardFees transactions by:
// 1. Rejecting user-submitted MsgForwardFees in CheckTx (protocol-injected only)
// 2. Deducting the fee from the fee address (not from a signer)
// 3. Sending the fee to the fee collector module
// 4. Setting context flags to skip signature verification (tx is unsigned)
//
// This decorator MUST be placed early in the ante chain, before:
// - DeductFeeDecorator (fee already deducted by this decorator)
// - SetPubKeyDecorator (no signers)
// - SigVerificationDecorator (no signatures)
// - IncrementSequenceDecorator (no signers)
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

	// Note: getMsgForwardFees already validates exactly one message exists.

	// Get the fee from the transaction
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "tx must implement FeeTx")
	}
	fee := feeTx.GetFee()

	if !fee.IsValid() || fee.IsZero() {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "fee forward tx must have non-zero fee")
	}

	// Fee must be exactly one coin in the native denom (defense-in-depth, also checked in ProcessProposal)
	if len(fee) != 1 {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, fmt.Sprintf("fee forward tx must have exactly one fee coin, got %d", len(fee)))
	}
	if fee[0].Denom != appconsts.BondDenom {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, fmt.Sprintf("fee forward tx must use %s, got %s", appconsts.BondDenom, fee[0].Denom))
	}

	// Deduct fee from fee address and send to fee collector
	err := d.bankKeeper.SendCoinsFromAccountToModule(ctx, feeaddresstypes.FeeAddress, authtypes.FeeCollectorName, fee)
	if err != nil {
		return ctx, errors.Wrap(err, "failed to deduct fee from fee address")
	}

	// Verify the context flag was set by EarlyFeeForwardDetector.
	// This is a defense-in-depth check to catch ante chain misconfiguration.
	// If this assertion fails, EarlyFeeForwardDetector is missing or misplaced.
	if !IsFeeForwardTx(ctx) {
		return ctx, errors.Wrap(sdkerrors.ErrLogic, "fee forward context flag not set; EarlyFeeForwardDetector missing from ante chain")
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
