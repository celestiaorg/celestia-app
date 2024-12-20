package ante

import (
	"context"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

var (
	_ sdk.AnteDecorator      = MsgVersioningGateKeeper{}
	_ baseapp.CircuitBreaker = MsgVersioningGateKeeper{}
)

// MsgVersioningGateKeeper dictates which transactions are accepted for an the app version
type MsgVersioningGateKeeper struct {
	// acceptedMsgs is a map from appVersion -> msgTypeURL -> struct{}.
	// If a msgTypeURL is present in the map it should be accepted for that appVersion.
	acceptedMsgs map[uint64]map[string]struct{}
}

func NewMsgVersioningGateKeeper(acceptedList map[uint64]map[string]struct{}) *MsgVersioningGateKeeper {
	return &MsgVersioningGateKeeper{
		acceptedMsgs: acceptedList,
	}
}

// AnteHandle implements the ante.Decorator interface
func (mgk MsgVersioningGateKeeper) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	acceptedMsgs, exists := mgk.acceptedMsgs[ctx.BlockHeader().Version.App]
	if !exists {
		return ctx, sdkerrors.ErrNotSupported.Wrapf("the app version %d is not supported", ctx.BlockHeader().Version.App)
	}

	if err := mgk.hasInvalidMsg(ctx, acceptedMsgs, tx.GetMsgs()); err != nil {
		return ctx, err
	}

	return next(ctx, tx, simulate)
}

func (mgk MsgVersioningGateKeeper) hasInvalidMsg(ctx sdk.Context, acceptedMsgs map[string]struct{}, msgs []sdk.Msg) error {
	for _, msg := range msgs {
		// Recursively check for invalid messages in nested authz messages.
		if execMsg, ok := msg.(*authz.MsgExec); ok {
			nestedMsgs, err := execMsg.GetMessages()
			if err != nil {
				return err
			}
			if err = mgk.hasInvalidMsg(ctx, acceptedMsgs, nestedMsgs); err != nil {
				return err
			}
		}

		msgTypeURL := sdk.MsgTypeURL(msg)
		_, exists := acceptedMsgs[msgTypeURL]
		if !exists {
			return sdkerrors.ErrNotSupported.Wrapf("message type %s is not supported in version %d", msgTypeURL, ctx.BlockHeader().Version.App)
		}
	}

	return nil
}

func (mgk MsgVersioningGateKeeper) IsAllowed(ctx context.Context, msgName string) (bool, error) {
	appVersion := sdk.UnwrapSDKContext(ctx).BlockHeader().Version.App
	acceptedMsgs, exists := mgk.acceptedMsgs[appVersion]
	if !exists {
		return false, sdkerrors.ErrNotSupported.Wrapf("app version %d is not supported", appVersion)
	}
	_, exists = acceptedMsgs[msgName]
	if !exists {
		return false, nil
	}
	return true, nil
}
