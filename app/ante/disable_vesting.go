package ante

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
)

var _ sdk.AnteDecorator = DisableVestingDecorator{}

// DisableVestingDecorator ensures that transactions containing MsgCreateVestingAccount
// are rejected if they were signed using amino JSON encoding.
type DisableVestingDecorator struct{}

// NewDisableVestingDecorator creates a new DisableVestingDecorator.
func NewDisableVestingDecorator() DisableVestingDecorator {
	return DisableVestingDecorator{}
}

// AnteHandle implements the AnteHandler interface. It checks if the transaction
// contains a MsgCreateVestingAccount and if it was signed using amino JSON.
// If both conditions are true, it rejects the transaction.
func (dvd DisableVestingDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// only applies to check tx
	if !ctx.IsCheckTx() {
		return next(ctx, tx, simulate)
	}

	// Check if any message in the transaction is MsgCreateVestingAccount
	hasVestingMsg := false
	for _, msg := range tx.GetMsgs() {
		if _, ok := msg.(*vestingtypes.MsgCreateVestingAccount); ok {
			hasVestingMsg = true
			break
		}
	}

	// If no vesting message found, continue to next handler
	if !hasVestingMsg {
		return next(ctx, tx, simulate)
	}

	// Check if transaction was signed using amino JSON
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, errors.Wrap(sdkerrors.ErrTxDecode, "invalid tx type")
	}

	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return ctx, err
	}

	// Check if any signature uses amino JSON signing mode using the existing cosmos-sdk function
	for _, sig := range sigs {
		if ante.OnlyLegacyAminoSigners(sig.Data) {
			return ctx, errors.Wrap(
				sdkerrors.ErrUnauthorized,
				"MsgCreateVestingAccount is temporarily disabled with amino JSON signing. "+
					"It will be re-enabled when the entire network upgrades. "+
					"MsgCreateVestingAccount transactions will work with direct sign mode.",
			)
		}
	}

	return next(ctx, tx, simulate)
}
