package paramfilter

import (
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	legacysdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
)

// NewParamChangeProposalHandler creates a new governance Handler for a ParamChangeProposal
func NewParamChangeProposalHandler(pfk Keeper, pk paramskeeper.Keeper) govtypes.Handler {
	return func(ctx sdk.Context, content govtypes.Content) error {
		switch c := content.(type) {
		case *proposal.ParameterChangeProposal:
			return handleParameterChangeProposal(ctx, pk, pfk, c)

		default:
			return sdkerrors.Wrapf(legacysdkerrors.ErrUnknownRequest, "unrecognized param proposal content type: %T", c)
		}
	}
}

func handleParameterChangeProposal(
	ctx sdk.Context,
	pk paramskeeper.Keeper,
	pfk Keeper,
	p *proposal.ParameterChangeProposal,
) error {
	// throw an error if any of the parameter changes are forbidden
	for _, c := range p.Changes {
		if pfk.IsForbidden(c.Subspace, c.Key) {
			return ErrForbiddenParameter
		}
	}

	for _, c := range p.Changes {
		ss, ok := pk.GetSubspace(c.Subspace)
		if !ok {
			return sdkerrors.Wrap(proposal.ErrUnknownSubspace, c.Subspace)
		}

		pk.Logger(ctx).Info(
			fmt.Sprintf("attempt to set new parameter value; key: %s, value: %s", c.Key, c.Value),
		)

		if err := ss.Update(ctx, []byte(c.Key), []byte(c.Value)); err != nil {
			return sdkerrors.Wrapf(proposal.ErrSettingParameter, "key: %s, value: %s, err: %s", c.Key, c.Value, err.Error())
		}
	}

	return nil
}
