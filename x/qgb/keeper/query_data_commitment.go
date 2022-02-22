package keeper

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k Keeper) DataCommitmentConfirm(c context.Context, request *types.QueryDataCommitmentConfirmRequest) (*types.QueryDataCommitmentConfirmResponse, error) {
	addr, err := sdk.AccAddressFromBech32(request.Address)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "address invalid")
	}
	return &types.QueryDataCommitmentConfirmResponse{Confirm: k.GetDataCommitmentConfirm(sdk.UnwrapSDKContext(c), request.Commitment, addr)}, nil
}

func (k Keeper) DataCommitmentConfirmsByValidator(ctx context.Context, request *types.QueryDataCommitmentConfirmsByValidatorRequest) (*types.QueryDataCommitmentConfirmsByValidatorResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (k Keeper) DataCommitmentConfirmsByCommitment(c context.Context, request *types.QueryDataCommitmentConfirmsByCommitmentRequest) (*types.QueryDataCommitmentConfirmsByCommitmentResponse, error) {
	return &types.QueryDataCommitmentConfirmsByCommitmentResponse{Confirm: k.GetDataCommitmentConfirmsByCommitment(sdk.UnwrapSDKContext(c), request.Commitment)}, nil
}

func (k Keeper) DataCommitmentConfirmsByRange(ctx context.Context, request *types.QueryDataCommitmentConfirmsByRangeRequest) (*types.QueryDataCommitmentConfirmsByRangeResponse, error) {
	//TODO implement me
	panic("implement me")
}
