package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

var _ sdk.AnteDecorator = MsgExecDecorator{}

// MsgExecDecorator ensures that the tx does not contain any nested MsgExec messages.
// Only applies to app version >= 4.
type MsgExecDecorator struct{}

func NewMsgExecDecorator() *MsgExecDecorator {
	return &MsgExecDecorator{}
}

func (mgk MsgExecDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	// This decorator is only applicable to app version 4 and above
	if ctx.BlockHeader().Version.App < 4 {
		return next(ctx, tx, simulate)
	}

	for _, msg := range tx.GetMsgs() {
		if msgExec, ok := msg.(*authz.MsgExec); ok {
			nestedMsgs, err := msgExec.GetMessages()
			if err != nil {
				return ctx, err
			}
			for _, nestedMsg := range nestedMsgs {
				if _, ok := nestedMsg.(*authz.MsgExec); ok {
					return ctx, sdkerrors.ErrNotSupported.Wrapf("MsgExec inside MsgExec is not supported")
				}
			}
		}
	}
	return next(ctx, tx, simulate)
}
