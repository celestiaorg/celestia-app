package keeper

import (
	"context"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// UpdateParams handles MsgUpdateParams: it gates on the configured authority,
// validates the proposed Params, persists them, and emits an EventUpdateParams.
func (k Keeper) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if msg.Authority != k.GetAuthority() {
		return nil, errors.Wrapf(sdkerrors.ErrUnauthorized, "invalid authority: expected: %s, got: %s", k.authority, msg.Authority)
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid parameters: %s", err)
	}

	k.SetParams(ctx, msg.Params)

	if err := ctx.EventManager().EmitTypedEvent(types.NewEventUpdateParams(msg.Authority, msg.Params)); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}
