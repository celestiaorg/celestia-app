package app

import (
	"fmt"
	"runtime/debug"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
)

// sigVerifyAnteHandler creates an AnteHandler with the SetupContext, SetPubKey,
// SigVerification, and IncremementSequence ante decorators to check that
// sequences have be incremented.
func sigVerifyAnteHandler(accKeeper *authkeeper.AccountKeeper, txConfig client.TxConfig) sdk.AnteHandler {
	setupd := ante.NewSetUpContextDecorator()
	setPubKd := ante.NewSetPubKeyDecorator(accKeeper)
	svd := ante.NewSigVerificationDecorator(accKeeper, txConfig.SignModeHandler())
	isd := ante.NewIncrementSequenceDecorator(accKeeper)
	return sdk.ChainAnteDecorators(setupd, setPubKd, svd, isd)
}

// incrementSequenceAnteHandler creates an AnteHandler that only incrememts the
// sequence.
func incrementSequenceAnteHandler(accKeeper *authkeeper.AccountKeeper) sdk.AnteHandler {
	setupd := ante.NewSetUpContextDecorator()
	isd := ante.NewIncrementSequenceDecorator(accKeeper)
	return sdk.ChainAnteDecorators(setupd, isd)
}

// recoverHandler will simply wrap the caught panic in an error containing the
// stack trace.
func recoverHandler(recoveryObj interface{}) error {
	return sdkerrors.Wrap(
		sdkerrors.ErrPanic, fmt.Sprintf(
			"recovered: %v\nstack:\n%v", recoveryObj, string(debug.Stack()),
		),
	)
}
