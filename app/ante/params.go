package ante

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// ParamFilter is a type alias for a filtering function which accepts an sdk.Msg and returns an error.
type ParamFilter func(sdk.Msg) error

// ParamFilterDecorator checks tx msgs for gov.MsgSubmitProposal and authz.MsgExec and ensures that param updates
// within these conform to the rules defines in paramFilters. ParamFilters are keyed by MsgTypeURL.
// NOTE: This replaces the param filter governance proposal handler from v3 and earlier.
type ParamFilterDecorator struct {
	paramFilters map[string]ParamFilter
}

// NewParamFilterDecorator creates and returns a new ParamFilterDecorator to be used in the ante handler chain.
func NewParamFilterDecorator(paramFilters map[string]ParamFilter) ParamFilterDecorator {
	return ParamFilterDecorator{
		paramFilters: paramFilters,
	}
}

// AnteHandle implements the AnteHandler interface.
// It ensures that MsgSubmitProposal has at least one message
// It ensures params are filtered within messages
func (d ParamFilterDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, m := range tx.GetMsgs() {
		if msgSubmitProp, ok := m.(*govv1.MsgSubmitProposal); ok {
			msgs, err := msgSubmitProp.GetMsgs()
			if err != nil {
				return ctx, err
			}

			if err := d.validateMsgs(msgs); err != nil {
				return ctx, err
			}
		}

		// we need to check if a gov proposal wasn't contained in a authz message
		if msgExec, ok := m.(*authz.MsgExec); ok {
			msgs, err := msgExec.GetMessages()
			if err != nil {
				return ctx, err
			}

			if err := d.validateMsgs(msgs); err != nil {
				return ctx, err
			}
		}
	}

	return next(ctx, tx, simulate)
}

// validateMsgs checks the nested messages within a `MsgSubmitProposal` or `MsgExec`.
// It ensures that:
// 1. At least one message is included in the proposal.
// 2. Recursively processes nested messages in case of `MsgExec` or `MsgSubmitProposal` types.
// 3. Applies the provided parameter filters to relevant messages, checking if parameter changes are allowed.
func (d ParamFilterDecorator) validateMsgs(msgs []sdk.Msg) error {
	if len(msgs) == 0 {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "must include at least one message")
	}

	for _, msg := range msgs {
		switch m := msg.(type) {
		case *authz.MsgExec:
			nested, err := m.GetMessages()
			if err != nil {
				return err
			}

			if err := d.validateMsgs(nested); err != nil {
				return err
			}
		case *govv1.MsgSubmitProposal:
			nested, err := m.GetMsgs()
			if err != nil {
				return err
			}

			if err := d.validateMsgs(nested); err != nil {
				return err
			}
		default:
			if paramFilter, found := d.paramFilters[sdk.MsgTypeURL(m)]; found {
				if err := paramFilter(m); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
