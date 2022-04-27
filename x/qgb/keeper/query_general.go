package keeper

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// LastUnbondingHeight queries the last unbonding height
func (k Keeper) LastUnbondingHeight(
	c context.Context,
	req *types.QueryLastUnbondingHeightRequest) (*types.QueryLastUnbondingHeightResponse, error) {
	return &types.QueryLastUnbondingHeightResponse{
		Height: k.GetLastUnBondingBlockHeight(sdk.UnwrapSDKContext(c)),
	}, nil
}
