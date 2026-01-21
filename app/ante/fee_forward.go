package ante

import (
	"cosmossdk.io/errors"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

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
	bankKeeper feeaddresstypes.FeeForwardBankKeeper
}

// NewFeeForwardDecorator creates a new FeeForwardDecorator.
func NewFeeForwardDecorator(bankKeeper feeaddresstypes.FeeForwardBankKeeper) *FeeForwardDecorator {
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
	// Reject in CheckTx, ReCheckTx, and simulation mode.
	if ctx.IsCheckTx() || ctx.IsReCheckTx() || simulate {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "MsgForwardFees cannot be submitted by users; it is protocol-injected only")
	}

	// Get the fee from the transaction
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "tx must implement FeeTx")
	}
	fee := feeTx.GetFee()

	// Validate fee format (defense-in-depth, also checked in ProcessProposal)
	if err := feeaddresstypes.ValidateFeeForwardFee(fee, nil); err != nil {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
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
