package ante

import (
	"cosmossdk.io/errors"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// FeeForwardTerminatorDecorator handles MsgForwardFees transactions completely and
// terminates the ante chain early. This decorator must be placed early in the chain
// (after SetUpContextDecorator) because MsgForwardFees has no signers and would fail
// signature-related decorators.
//
// For MsgForwardFees transactions, this decorator:
// 1. Rejects user submissions (only valid when protocol-injected)
// 2. Validates the fee format
// 3. Transfers the fee from fee address to fee collector
// 4. Returns without calling next() - skipping the rest of the ante chain
//
// For all other transactions, this decorator simply calls next().
type FeeForwardTerminatorDecorator struct {
	bankKeeper feeaddresstypes.FeeForwardBankKeeper
}

// NewFeeForwardTerminatorDecorator creates a new FeeForwardTerminatorDecorator.
func NewFeeForwardTerminatorDecorator(bankKeeper feeaddresstypes.FeeForwardBankKeeper) *FeeForwardTerminatorDecorator {
	return &FeeForwardTerminatorDecorator{
		bankKeeper: bankKeeper,
	}
}

// AnteHandle implements sdk.AnteDecorator.
func (d FeeForwardTerminatorDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := feeaddresstypes.IsFeeForwardMsg(tx)
	if msg == nil {
		// Not a fee forward tx - continue with normal ante chain
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
	ctx = ctx.WithValue(feeaddresstypes.FeeForwardAmountContextKey{}, fee)

	// Terminate the ante chain - MsgForwardFees is fully handled.
	// We don't call next() because:
	// - No signatures to verify (protocol-injected)
	// - Fee already deducted above
	// - No sequence to increment (no signers)
	return ctx, nil
}
