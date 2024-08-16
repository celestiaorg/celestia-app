package ante

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gov "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// GovProposalDecorator ensures that a tx with a MsgSubmitProposal has at least
// one message in the proposal.
type GovProposalDecorator struct{}

func NewGovProposalDecorator() GovProposalDecorator {
	return GovProposalDecorator{}
}

// AnteHandle implements the AnteHandler interface. It ensures that MsgSubmitProposal has at least one message
func (d GovProposalDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, m := range tx.GetMsgs() {
		if proposal, ok := m.(*govv1.MsgSubmitProposal); ok {
			if len(proposal.Messages) == 0 {
				return ctx, errors.Wrapf(gov.ErrNoProposalMsgs, "must include at least one message in proposal")
			}
		}
	}

	return next(ctx, tx, simulate)
}
