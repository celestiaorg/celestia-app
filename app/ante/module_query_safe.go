package ante

import (
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
)

var _ sdk.AnteDecorator = ModuleQuerySafeRequestsDecorator{}

// ModuleQuerySafeRequestsDecorator rejects a MsgModuleQuerySafe with more than
// MaxModuleQuerySafeRequests requests. Each request counts as one message
// against the per-block MaxSDKMessages budget but costs only a few bytes, so
// without a bound one small tx could take the whole budget. Only one level of
// MsgExec and MsgSubmitProposal is inspected because NestedMsgDecorator rejects
// deeper nesting, and the default ICA allowlist does not carry this message.
type ModuleQuerySafeRequestsDecorator struct{}

func NewModuleQuerySafeRequestsDecorator() ModuleQuerySafeRequestsDecorator {
	return ModuleQuerySafeRequestsDecorator{}
}

func (ModuleQuerySafeRequestsDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		if err := checkModuleQuerySafeRequests(msg); err != nil {
			return ctx, err
		}
		var nested []sdk.Msg
		var err error
		switch m := msg.(type) {
		case *authz.MsgExec:
			nested, err = m.GetMessages()
		case *govv1.MsgSubmitProposal:
			nested, err = m.GetMsgs()
		}
		if err != nil {
			return ctx, err
		}
		for _, nestedMsg := range nested {
			if err := checkModuleQuerySafeRequests(nestedMsg); err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}

func checkModuleQuerySafeRequests(msg sdk.Msg) error {
	m, ok := msg.(*icahosttypes.MsgModuleQuerySafe)
	if !ok {
		return nil
	}
	if len(m.Requests) > appconsts.MaxModuleQuerySafeRequests {
		return sdkerrors.ErrInvalidRequest.Wrapf("MsgModuleQuerySafe has %d requests, limit is %d", len(m.Requests), appconsts.MaxModuleQuerySafeRequests)
	}
	return nil
}
