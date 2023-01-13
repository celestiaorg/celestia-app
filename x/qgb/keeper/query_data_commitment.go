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
	return &types.QueryDataCommitmentRangeForHeightResponse{
		BeginBlock: resp.BeginBlock,
		EndBlock:   resp.EndBlock,
		Nonce:      resp.Nonce,
	}, nil
}
