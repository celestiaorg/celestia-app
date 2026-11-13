package ante

import (
	evidencetypes "cosmossdk.io/x/evidence/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
)

var _ sdk.AnteDecorator = UnsupportedMsgDecorator{}

// UnsupportedMsgDecorator rejects messages that are routed but can never
// succeed with this app's configuration, at the top level and inside a
// MsgExec or MsgSubmitProposal:
//
//   - MsgSubmitEvidence: the evidence keeper has no evidence router.
//   - MsgExecLegacyContent and gov v1beta1 MsgSubmitProposal: the gov legacy
//     proposal router has no routes.
//   - MsgCreatePeriodicVestingAccount and MsgCreatePermanentLockedAccount: the
//     cosmos-sdk fork's handlers always return an error.
//   - MsgChannelCloseInit: the transfer and ICA host callbacks reject
//     user-initiated channel closing.
//   - MsgChannelUpgradeInit: the transfer stack is not upgradable and the ICA
//     host rejects host-initiated upgrades.
//
// Rejecting them in the ante handler keeps them out of the mempool and blocks
// instead of charging fees for a message that always fails.
type UnsupportedMsgDecorator struct{}

func NewUnsupportedMsgDecorator() *UnsupportedMsgDecorator {
	return &UnsupportedMsgDecorator{}
}

func (d UnsupportedMsgDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msgs := tx.GetMsgs()
	if err := rejectUnsupported(msgs, ""); err != nil {
		return ctx, err
	}
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *authz.MsgExec:
			nested, err := m.GetMessages()
			if err != nil {
				return ctx, err
			}
			if err := rejectUnsupported(nested, " inside MsgExec"); err != nil {
				return ctx, err
			}
		case *govv1.MsgSubmitProposal:
			nested, err := m.GetMsgs()
			if err != nil {
				return ctx, err
			}
			if err := rejectUnsupported(nested, " inside MsgSubmitProposal"); err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}

// rejectUnsupported returns an error for the first message in msgs that can
// never succeed.
func rejectUnsupported(msgs []sdk.Msg, where string) error {
	for _, msg := range msgs {
		switch msg.(type) {
		case *evidencetypes.MsgSubmitEvidence,
			*govv1.MsgExecLegacyContent,
			*govv1beta1.MsgSubmitProposal,
			*vestingtypes.MsgCreatePeriodicVestingAccount,
			*vestingtypes.MsgCreatePermanentLockedAccount,
			*channeltypes.MsgChannelCloseInit,
			*channeltypes.MsgChannelUpgradeInit:
			return sdkerrors.ErrNotSupported.Wrapf("%s%s is not supported", sdk.MsgTypeURL(msg), where)
		}
	}
	return nil
}
