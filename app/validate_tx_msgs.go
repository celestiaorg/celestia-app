package app

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// validateTxMsgs runs the message checks that baseapp runs in FinalizeBlock
// before the ante handler: the tx has at least one message, every message
// passes ValidateBasic and has a registered handler. A tx that fails them
// fails in FinalizeBlock without paying fees or incrementing its sequence, so
// PrepareProposal drops it and ProcessProposal rejects a block containing it.
//
// A panic in a message's ValidateBasic is returned as an error, as baseapp
// does. A nil router skips the handler check; only unit tests pass one.
func validateTxMsgs(sdkTx sdk.Tx, router baseapp.MessageRouter) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errorsmod.Wrap(sdkerrors.ErrPanic, fmt.Sprintf("message validation: %v", r))
		}
	}()

	msgs := sdkTx.GetMsgs()
	if len(msgs) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "must contain at least one message")
	}
	for _, msg := range msgs {
		if m, ok := msg.(sdk.HasValidateBasic); ok {
			if err := m.ValidateBasic(); err != nil {
				return err
			}
		}
	}
	if router == nil {
		return nil
	}
	for _, msg := range msgs {
		if router.Handler(msg) == nil {
			return errorsmod.Wrapf(sdkerrors.ErrUnknownRequest, "no message handler found for %T", msg)
		}
	}
	return nil
}
