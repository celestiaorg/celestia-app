package ante

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v7/app/params"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// FeeForwardContextKey is a context key that indicates a transaction is a
// protocol-injected fee forward transaction. When this key is set to true,
// downstream ante decorators (DeductFeeDecorator, signature verification, etc.)
// should skip their processing since the fee has already been handled and
// the transaction is unsigned by design.
type FeeForwardContextKey struct{}

// FeeForwardAmountContextKey is a context key that stores the fee amount
// deducted from the fee address by the FeeForwardDecorator. The keeper's
// ForwardFees message handler reads this value to emit the EventFeeForwarded
// event with the correct amount. This coupling is intentional - the ante
// handler handles the actual fee transfer, while the message handler emits
// the event for tracking purposes.
type FeeForwardAmountContextKey struct{}

// FeeForwardBankKeeper defines the bank keeper interface needed by FeeForwardDecorator.
type FeeForwardBankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

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
	msg := getMsgForwardFees(tx)
	if msg == nil {
		return next(ctx, tx, simulate)
	}

	// MsgForwardFees MUST NOT be submitted by users directly (CIP-43).
	// It is only valid when injected by the block proposer in PrepareProposal.
	if ctx.IsCheckTx() || ctx.IsReCheckTx() {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "MsgForwardFees cannot be submitted by users; it is protocol-injected only")
	}

	// Validate that there's exactly one message and it's MsgForwardFees
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "fee forward tx must have exactly one message")
	}

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
	if fee[0].Denom != params.BondDenom {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, fmt.Sprintf("fee forward tx must use %s, got %s", params.BondDenom, fee[0].Denom))
	}

	// Deduct fee from fee address and send to fee collector
	err := d.bankKeeper.SendCoinsFromAccountToModule(ctx, feeaddresstypes.FeeAddress, authtypes.FeeCollectorName, fee)
	if err != nil {
		return ctx, errors.Wrap(err, "failed to deduct fee from fee address")
	}

	// Set context flag to skip remaining fee/sig decorators
	ctx = ctx.WithValue(FeeForwardContextKey{}, true)

	// Store the fee amount in context for the message handler to emit the event
	ctx = ctx.WithValue(FeeForwardAmountContextKey{}, fee)

	return next(ctx, tx, simulate)
}

// getMsgForwardFees returns the MsgForwardFees if the transaction contains one, nil otherwise.
func getMsgForwardFees(tx sdk.Tx) *feeaddresstypes.MsgForwardFees {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return nil
	}
	msg, ok := msgs[0].(*feeaddresstypes.MsgForwardFees)
	if !ok {
		return nil
	}
	return msg
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

// ContainsMsgForwardFees checks if a transaction contains a MsgForwardFees message.
func ContainsMsgForwardFees(tx sdk.Tx) bool {
	return getMsgForwardFees(tx) != nil
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
