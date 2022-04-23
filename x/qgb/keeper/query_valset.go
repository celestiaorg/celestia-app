package keeper

import (
	"context"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// LastValsetRequests queries the LastValsetRequests of the qgb module
func (k Keeper) LastValsetRequests(
	c context.Context,
	req *types.QueryLastValsetRequestsRequest) (*types.QueryLastValsetRequestsResponse, error) {
	valReq := k.GetValsets(sdk.UnwrapSDKContext(c))
	valReqLen := len(valReq)
	retLen := 0
	if valReqLen < maxValsetRequestsReturned {
		retLen = valReqLen
	} else {
		retLen = maxValsetRequestsReturned
	}
	// TODO: check if we need the first ones or the last ones
	return &types.QueryLastValsetRequestsResponse{Valsets: valReq[0:retLen]}, nil
}

// ValsetRequestByNonce queries the Valset request of the qgb module by nonce
func (k Keeper) ValsetRequestByNonce(
	c context.Context,
	req *types.QueryValsetRequestByNonceRequest) (*types.QueryValsetRequestByNonceResponse, error) {
	valReq := k.GetValsets(sdk.UnwrapSDKContext(c))
	for _, valset := range valReq {
		if valset.Nonce == req.Nonce {
			vs, err := types.CopyValset(valset)
			if err != nil {
				return nil, err
			}
			return &types.QueryValsetRequestByNonceResponse{Valset: vs}, nil
		}
	}

	return nil, sdkerrors.Wrap(sdkerrors.ErrNotFound, "valset request nonce not found")
}
