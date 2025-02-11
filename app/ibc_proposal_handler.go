package app

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"

	"github.com/cosmos/ibc-go/v8/modules/core/02-client/keeper"
	"github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
)

// NewClientProposalHandler defines the 02-client proposal handler. It disables the
// UpgradeProposalType. Handling of updating the IBC Client will be done in v2 of the
// app.
// TODO(review): This can be removed completely in favor of govv1 messaging.
func NewClientProposalHandler(k keeper.Keeper) govtypes.Handler {
	return func(ctx sdk.Context, content govtypes.Content) error {
		switch c := content.(type) {
		case *types.ClientUpdateProposal:
			return k.RecoverClient(ctx, c.SubjectClientId, c.SubstituteClientId)
		case *types.UpgradeProposal:
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "ibc upgrade proposal not supported")

		default:
			return errors.Wrapf(sdkerrors.ErrUnknownRequest, "unrecognized ibc proposal content type: %T", c)
		}
	}
}
