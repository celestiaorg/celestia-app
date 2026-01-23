package ante

import (
	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// ProtocolFeeTerminatorDecorator handles MsgPayProtocolFee transactions completely and
// terminates the ante chain early. This decorator must be placed early in the chain
// (after SetUpContextDecorator) because MsgPayProtocolFee has no signers and would fail
// signature-related decorators.
//
// For MsgPayProtocolFee transactions, this decorator:
// 1. Rejects user submissions (only valid when protocol-injected)
// 2. Validates the fee format
// 3. Transfers the fee from fee address to fee collector
// 4. Emits EventProtocolFeePaid
// 5. Returns without calling next() - skipping the rest of the ante chain
//
// For all other transactions, this decorator simply calls next().
type ProtocolFeeTerminatorDecorator struct {
	bankKeeper types.ProtocolFeeBankKeeper
}

// NewProtocolFeeTerminatorDecorator creates a new ProtocolFeeTerminatorDecorator.
func NewProtocolFeeTerminatorDecorator(bankKeeper types.ProtocolFeeBankKeeper) *ProtocolFeeTerminatorDecorator {
	if bankKeeper == nil {
		panic("bankKeeper cannot be nil")
	}
	return &ProtocolFeeTerminatorDecorator{
		bankKeeper: bankKeeper,
	}
}

// AnteHandle implements sdk.AnteDecorator.
func (d ProtocolFeeTerminatorDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := types.IsProtocolFeeMsg(tx)
	if msg == nil {
		return next(ctx, tx, simulate)
	}

	// MsgPayProtocolFee MUST NOT be submitted by users directly (CIP-43).
	// It is only valid when injected by the block proposer in PrepareProposal.
	if ctx.IsCheckTx() || ctx.IsReCheckTx() || simulate {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "MsgPayProtocolFee cannot be submitted by users; it is protocol-injected only")
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "tx must implement FeeTx")
	}
	fee := feeTx.GetFee()

	if err := types.ValidateProtocolFee(fee, nil); err != nil {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	err := d.bankKeeper.SendCoinsFromAccountToModule(ctx, types.FeeAddress, authtypes.FeeCollectorName, fee)
	if err != nil {
		return ctx, errors.Wrap(err, "failed to deduct fee from fee address")
	}

	if err := ctx.EventManager().EmitTypedEvent(types.NewProtocolFeePaidEvent(types.FeeAddressBech32, fee.String())); err != nil {
		return ctx, errors.Wrap(err, "failed to emit protocol feeed event")
	}

	return ctx, nil
}
