package ante

import (
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

var _ sdk.AnteDecorator = MsgExecDecorator{}

// MsgExecDecorator ensures that the tx does not contain a MsgExec with a
// nested MsgExec or MsgPayForBlobs.
type MsgExecDecorator struct{}

func NewMsgExecDecorator() *MsgExecDecorator {
	return &MsgExecDecorator{}
}

func (mgk MsgExecDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
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
				if _, ok := nestedMsg.(*blobtypes.MsgPayForBlobs); ok {
					return ctx, sdkerrors.ErrNotSupported.Wrapf("MsgPayForBlobs inside MsgExec is not supported")
				}
			}
		}
	}
	return next(ctx, tx, simulate)
}
