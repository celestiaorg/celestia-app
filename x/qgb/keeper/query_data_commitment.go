package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k Keeper) DataCommitmentRangeForHeight(
	c context.Context,
	request *types.QueryDataCommitmentRangeForHeightRequest,
) (*types.QueryDataCommitmentRangeForHeightResponse, error) {
	resp, err := k.GetDataCommitmentForHeight(sdk.UnwrapSDKContext(c), request.Height)
	if err != nil {
		return nil, err
	}
	return &types.QueryDataCommitmentRangeForHeightResponse{DataCommitment: &resp}, nil
}

func (k Keeper) LastDataCommitment(
	c context.Context,
	_ *types.QueryLastDataCommitmentRequest,
) (*types.QueryLastDataCommitmentResponse, error) {
	resp, err := k.GetLastDataCommitment(sdk.UnwrapSDKContext(c))
	if err != nil {
		return nil, err
	}
	return &types.QueryLastDataCommitmentResponse{
		DataCommitment: &resp,
	}, nil
}
