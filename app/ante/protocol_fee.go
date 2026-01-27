package ante

import (
	"fmt"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/pkg/feeaddress"
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
// 2. Validates fee == fee address balance (ensures ALL funds are forwarded)
// 3. Validates gas == ProtocolFeeGasLimit
// 4. Transfers the fee from fee address to fee collector
// 5. Returns without calling next() - skipping the rest of the ante chain
//
// For all other transactions, this decorator simply calls next().
type ProtocolFeeTerminatorDecorator struct {
	bankKeeper feeaddress.ProtocolFeeBankKeeper
}

// NewProtocolFeeTerminatorDecorator creates a new ProtocolFeeTerminatorDecorator.
func NewProtocolFeeTerminatorDecorator(bankKeeper feeaddress.ProtocolFeeBankKeeper) *ProtocolFeeTerminatorDecorator {
	if bankKeeper == nil {
		panic("bankKeeper cannot be nil")
	}
	return &ProtocolFeeTerminatorDecorator{
		bankKeeper: bankKeeper,
	}
}

// AnteHandle implements sdk.AnteDecorator.
func (d ProtocolFeeTerminatorDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := feeaddress.IsProtocolFeeMsg(tx)
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

	// Get current fee address balance - the fee MUST equal this balance exactly.
	// This ensures ALL accumulated funds are forwarded to validators, preventing
	// a malicious proposer from only forwarding a partial amount.
	feeAddressBalance := d.bankKeeper.GetBalance(ctx, feeaddress.FeeAddress, appconsts.BondDenom)
	if err := feeaddress.ValidateProtocolFee(fee, &feeAddressBalance); err != nil {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	// Validate gas limit matches expected constant
	if feeTx.GetGas() != feeaddress.ProtocolFeeGasLimit {
		return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest,
			fmt.Sprintf("gas limit %d does not match expected %d", feeTx.GetGas(), feeaddress.ProtocolFeeGasLimit))
	}

	err := d.bankKeeper.SendCoinsFromAccountToModule(ctx, feeaddress.FeeAddress, authtypes.FeeCollectorName, fee)
	if err != nil {
		return ctx, errors.Wrap(err, "failed to deduct fee from fee address")
	}

	return ctx, nil
}
