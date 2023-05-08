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

// ParamBlockList keeps track of parameters that cannot be changed by governance
// proposals
type ParamBlockList struct {
	params map[string]bool
}

// NewParamBlockList creates a new ParamBlockList that can be used to block gov
// proposals that attempt to change locked parameters.
func NewParamBlockList(blockedParams ...[2]string) ParamBlockList {
	consolidatedParams := make(map[string]bool, len(blockedParams))
	for _, param := range blockedParams {
		consolidatedParams[fmt.Sprintf("%s-%s", param[0], param[1])] = true
	}
	return ParamBlockList{params: consolidatedParams}
}

// IsBlocked returns true if the given parameter is blocked.
func (pbl ParamBlockList) IsBlocked(subspace string, key string) bool {
	return pbl.params[fmt.Sprintf("%s-%s", subspace, key)]
}

// GovHandler creates a new governance Handler for a ParamChangeProposal using
// the underlying ParamBlockList.
func (pbl ParamBlockList) GovHandler(pk paramskeeper.Keeper) govtypes.Handler {
	return func(ctx sdk.Context, content govtypes.Content) error {
		switch c := content.(type) {
		case *proposal.ParameterChangeProposal:
			return pbl.handleParameterChangeProposal(ctx, pk, c)

		default:
			return sdkerrors.Wrapf(legacysdkerrors.ErrUnknownRequest, "unrecognized param proposal content type: %T", c)
		}
	}
}

func (pbl ParamBlockList) handleParameterChangeProposal(
	ctx sdk.Context,
	pk paramskeeper.Keeper,
	p *proposal.ParameterChangeProposal,
) error {
	// throw an error if any of the parameter changes are blocked
	for _, c := range p.Changes {
		if pbl.IsBlocked(c.Subspace, c.Key) {
			return ErrBlockedParameter
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
