package ante

import (
	"bytes"
	"context"
	"encoding/hex"

	"cosmossdk.io/errors"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// FeeForwardContextKey is the context key used to indicate that a transaction
// is a fee forward transaction and should skip certain ante decorators.
type FeeForwardContextKey struct{}

// FeeForwardAmountContextKey is the context key used to store the fee amount
// deducted from the fee address, so the message handler can emit the event.
type FeeForwardAmountContextKey struct{}

// FeeForwardBankKeeper defines the bank keeper interface needed by FeeForwardDecorator.
type FeeForwardBankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

// FeeForwardDecorator handles MsgForwardFees transactions by:
// 1. Validating the proposer matches the block proposer
// 2. Deducting the fee from the fee address (not from signer)
// 3. Setting a context flag to skip signature verification
//
// This decorator MUST be placed early in the ante chain, before:
// - DeductFeeDecorator
// - SetPubKeyDecorator
// - SigVerificationDecorator
// - IncrementSequenceDecorator
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

	// Validate that there's exactly one message and it's MsgForwardFees
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "fee forward tx must have exactly one message")
	}

	// Validate proposer matches block proposer
	blockProposer := ctx.BlockHeader().ProposerAddress
	msgProposer, err := hex.DecodeString(msg.Proposer)
	if err != nil {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid proposer address encoding")
	}
	if !bytes.Equal(blockProposer, msgProposer) {
		return ctx, errors.Wrap(sdkerrors.ErrUnauthorized, "proposer does not match block proposer")
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

	// Deduct fee from fee address and send to fee collector
	err = d.bankKeeper.SendCoinsFromAccountToModule(ctx, feeaddresstypes.FeeAddress, authtypes.FeeCollectorName, fee)
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
