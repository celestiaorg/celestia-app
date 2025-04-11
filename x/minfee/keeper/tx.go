package keeper

import (
	"context"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/celestiaorg/celestia-app/v4/x/minfee/types"
)

var _ types.MsgServer = (*Keeper)(nil)

// UpdateMinfeeParams updates minfee module parameters.
func (k Keeper) UpdateMinfeeParams(goCtx context.Context, msg *types.MsgUpdateMinfeeParams) (*types.MsgUpdateMinfeeParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// ensure that the sender has the authority to update the parameters.
	if msg.Authority != k.GetAuthority() {
		return nil, errors.Wrapf(sdkerrors.ErrUnauthorized, "invalid authority: expected: %s, got: %s", k.authority, msg.Authority)
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid parameters: %s", err)
	}

	k.SetParams(ctx, msg.Params)

	// Emit an event indicating successful parameter update.
	if err := ctx.EventManager().EmitTypedEvent(
		types.NewUpdateMinfeeParamsEvent(msg.Authority, msg.Params),
	); err != nil {
		return nil, err
	}

	return &types.MsgUpdateMinfeeParamsResponse{}, nil
}
