package ante

import (
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

var _ sdk.AnteDecorator = NestedMsgDecorator{}

// NestedMsgDecorator rejects messages wrapped in a MsgExec or MsgSubmitProposal
// that must not skip the ante handler: MsgPayForBlobs, MsgPayForFibre, and the
// fibre escrow messages (which would otherwise bypass PFF signature verification
// or the proposal-time settlement replay). It also rejects a nested MsgExec or
// MsgSubmitProposal so those can't be smuggled one level deeper.
type NestedMsgDecorator struct{}

func NewNestedMsgDecorator() *NestedMsgDecorator {
	return &NestedMsgDecorator{}
}

func (d NestedMsgDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {
		case *authz.MsgExec:
			nested, err := m.GetMessages()
			if err != nil {
				return ctx, err
			}
			if err := rejectWrapped(nested, "MsgExec"); err != nil {
				return ctx, err
			}
		case *govv1.MsgSubmitProposal:
			nested, err := m.GetMsgs()
			if err != nil {
				return ctx, err
			}
			if err := rejectWrapped(nested, "MsgSubmitProposal"); err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}

// rejectWrapped rejects messages that are not allowed inside container.
func rejectWrapped(msgs []sdk.Msg, container string) error {
	for _, msg := range msgs {
		switch msg.(type) {
		case *blobtypes.MsgPayForBlobs:
			return sdkerrors.ErrNotSupported.Wrapf("MsgPayForBlobs inside %s is not supported", container)
		case *fibretypes.MsgPayForFibre:
			return sdkerrors.ErrNotSupported.Wrapf("MsgPayForFibre inside %s is not supported", container)
		case *fibretypes.MsgPaymentPromiseTimeout:
			return sdkerrors.ErrNotSupported.Wrapf("MsgPaymentPromiseTimeout inside %s is not supported", container)
		case *fibretypes.MsgDepositToEscrow:
			return sdkerrors.ErrNotSupported.Wrapf("MsgDepositToEscrow inside %s is not supported", container)
		case *fibretypes.MsgRequestWithdrawal:
			return sdkerrors.ErrNotSupported.Wrapf("MsgRequestWithdrawal inside %s is not supported", container)
		case *authz.MsgExec:
			return sdkerrors.ErrNotSupported.Wrapf("MsgExec inside %s is not supported", container)
		case *govv1.MsgSubmitProposal:
			return sdkerrors.ErrNotSupported.Wrapf("MsgSubmitProposal inside %s is not supported", container)
		}
	}
	return nil
}
