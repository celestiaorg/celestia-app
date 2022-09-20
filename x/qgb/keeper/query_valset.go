package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TODO add unit tests for all of these requests

// LastValsetRequestBeforeNonce queries the last valset request before nonce
func (k Keeper) LastValsetRequestBeforeNonce(
	c context.Context,
	req *types.QueryLastValsetRequestBeforeNonceRequest,
) (*types.QueryLastValsetRequestBeforeNonceResponse, error) {
	vs, err := k.GetLastValsetBeforeNonce(sdk.UnwrapSDKContext(c), req.Nonce)
	if err != nil {
		return nil, err
	}
	return &types.QueryLastValsetRequestBeforeNonceResponse{Valset: vs}, nil
}
