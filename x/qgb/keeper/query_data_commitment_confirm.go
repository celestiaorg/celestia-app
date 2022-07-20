package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// FIXME should we return an error if it doesn't exist? for this one and the others?
func (k Keeper) DataCommitmentConfirm(
	c context.Context,
	request *types.QueryDataCommitmentConfirmRequest,
) (*types.QueryDataCommitmentConfirmResponse, error) {
	addr, err := sdk.AccAddressFromBech32(request.Address)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "address invalid")
	}
	// _ because if the confirm is not found, the method will return nil
	// and we want to bubble the nil to the user.
	confirm, _, err := k.GetDataCommitmentConfirm(
		sdk.UnwrapSDKContext(c),
		request.EndBlock,
		request.BeginBlock,
		addr,
	)
	if err != nil {
		return nil, err
	}
	return &types.QueryDataCommitmentConfirmResponse{Confirm: confirm}, nil
}

func (k Keeper) DataCommitmentConfirmsByCommitment(
	ctx context.Context,
	request *types.QueryDataCommitmentConfirmsByCommitmentRequest,
) (*types.QueryDataCommitmentConfirmsByCommitmentResponse, error) {
	return &types.QueryDataCommitmentConfirmsByCommitmentResponse{
		Confirms: k.GetDataCommitmentConfirmsByCommitment(
			sdk.UnwrapSDKContext(ctx),
			request.Commitment,
		),
	}, nil
}

func (k Keeper) DataCommitmentConfirmsByExactRange(
	ctx context.Context,
	request *types.QueryDataCommitmentConfirmsByExactRangeRequest,
) (*types.QueryDataCommitmentConfirmsByExactRangeResponse, error) {
	return &types.QueryDataCommitmentConfirmsByExactRangeResponse{
		Confirms: k.GetDataCommitmentConfirmsByExactRange(
			sdk.UnwrapSDKContext(ctx),
			request.BeginBlock,
			request.EndBlock,
		),
	}, nil
}
