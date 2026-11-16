package app

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// govEndBlockKey marks the context of the x/gov EndBlocker.
type govEndBlockKey struct{}

// EndBlock runs the x/gov EndBlocker with a context that tells govMsgRouter
// to replace the errors of proposal messages.
func (m govModule) EndBlock(ctx context.Context) error {
	return m.AppModule.EndBlock(sdk.UnwrapSDKContext(ctx).WithValue(govEndBlockKey{}, true))
}

// govMsgRouter is the message router of the x/gov keeper. x/gov stores the
// text of a proposal message's error, or of its recovered panic, in
// Proposal.FailedReason. That text can hold Go-specific details (source
// file frames, pointer addresses), so in the EndBlocker the router replaces
// it with the error's ABCI codespace and code. Other callers, such as
// MsgSubmitProposal, see the original handler.
type govMsgRouter struct {
	baseapp.MessageRouter
}

// Handler implements baseapp.MessageRouter.
func (r govMsgRouter) Handler(msg sdk.Msg) baseapp.MsgServiceHandler {
	return govMsgHandler(r.MessageRouter.Handler(msg))
}

// HandlerByTypeURL implements baseapp.MessageRouter.
func (r govMsgRouter) HandlerByTypeURL(typeURL string) baseapp.MsgServiceHandler {
	return govMsgHandler(r.MessageRouter.HandlerByTypeURL(typeURL))
}

func govMsgHandler(handler baseapp.MsgServiceHandler) baseapp.MsgServiceHandler {
	if handler == nil {
		return nil
	}
	return func(ctx sdk.Context, msg sdk.Msg) (res *sdk.Result, err error) {
		if ctx.Value(govEndBlockKey{}) == nil {
			return handler(ctx, msg)
		}
		// x/gov recovers a panic and stores its value as text. Recover it
		// here first so that only the panic code is stored.
		defer func() {
			if r := recover(); r != nil {
				res, err = nil, govMsgError{cause: errorsmod.ErrPanic}
			}
		}()
		res, err = handler(ctx, msg)
		if err != nil {
			return nil, govMsgError{cause: err}
		}
		return res, nil
	}
}

// govMsgError keeps the cause of a proposal message error, so its ABCI code
// and errors.Is are unchanged, but its text has only the codespace and code.
type govMsgError struct {
	cause error
}

func (e govMsgError) Error() string {
	codespace, code, _ := errorsmod.ABCIInfo(e.cause, false)
	return fmt.Sprintf("proposal message failed: codespace %s, code %d", codespace, code)
}

func (e govMsgError) Cause() error { return e.cause }

func (e govMsgError) Unwrap() error { return e.cause }
