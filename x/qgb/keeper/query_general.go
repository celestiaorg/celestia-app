package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// LastUnbondingHeight queries the last unbonding height.
func (k Keeper) LastUnbondingHeight(
	c context.Context,
	req *types.QueryLastUnbondingHeightRequest,
) (*types.QueryLastUnbondingHeightResponse, error) {
	return &types.QueryLastUnbondingHeightResponse{
		Height: k.GetLastUnBondingBlockHeight(sdk.UnwrapSDKContext(c)),
	}, nil
}

func (k Keeper) Params(c context.Context, request *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params := k.GetParams(sdk.UnwrapSDKContext(c))
	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}

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
