package ante

import (
	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
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
// 4. Emits EventFeeForwarded
// 5. Returns without calling next() - skipping the rest of the ante chain
//
// For all other transactions, this decorator simply calls next().
type FeeForwardTerminatorDecorator struct {
	bankKeeper types.FeeForwardBankKeeper
}

// NewFeeForwardTerminatorDecorator creates a new FeeForwardTerminatorDecorator.
func NewFeeForwardTerminatorDecorator(bankKeeper types.FeeForwardBankKeeper) *FeeForwardTerminatorDecorator {
	if bankKeeper == nil {
		panic("bankKeeper cannot be nil")
	}
	return &FeeForwardTerminatorDecorator{
		bankKeeper: bankKeeper,
	}
}

// AnteHandle implements sdk.AnteDecorator.
func (d FeeForwardTerminatorDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := types.IsFeeForwardMsg(tx)
	if msg == nil {
		return next(ctx, tx, simulate)
	}

	// MsgForwardFees MUST NOT be submitted by users directly (CIP-43).
	// It is only valid when injected by the block proposer in PrepareProposal.
	if ctx.IsCheckTx() || ctx.IsReCheckTx() || simulate {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "MsgForwardFees cannot be submitted by users; it is protocol-injected only")
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "tx must implement FeeTx")
	}
	fee := feeTx.GetFee()

	if err := types.ValidateFeeForwardFee(fee, nil); err != nil {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	err := d.bankKeeper.SendCoinsFromAccountToModule(ctx, types.FeeAddress, authtypes.FeeCollectorName, fee)
	if err != nil {
		return ctx, errors.Wrap(err, "failed to deduct fee from fee address")
	}

	if err := ctx.EventManager().EmitTypedEvent(types.NewFeeForwardedEvent(types.FeeAddressBech32, fee.String())); err != nil {
		return ctx, errors.Wrap(err, "failed to emit fee forwarded event")
	}

	// Terminate the ante chain - MsgForwardFees is fully handled.
	// We don't call next() because:
	// - No signatures to verify (protocol-injected)
	// - Fee already deducted above
	// - No sequence to increment (no signers)
	return ctx, nil
}
