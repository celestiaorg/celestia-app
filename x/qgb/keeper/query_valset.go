package keeper

import (
	"context"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TODO add unit tests for all of these requests
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
	// TODO add test for this
	return &types.QueryValsetRequestByNonceResponse{Valset: k.GetValset(
		sdk.UnwrapSDKContext(c),
		req.Nonce,
	)}, nil
}

// LastValsetBeforeHeight queries the last valset request before height
func (k Keeper) LastValsetBeforeHeight(
	c context.Context,
	req *types.QueryLastValsetBeforeHeightRequest) (*types.QueryLastValsetBeforeHeightResponse, error) {
	valReq := k.GetValsets(sdk.UnwrapSDKContext(c))
	for _, valset := range valReq {
		// The first check is correct because we will always have a valset at block 0.
		// We're creating valsets:
		//  - If we have no valset
		//	- We're an unbonding height
		//	- There was a significant power difference in the validator set
		// For more information, check qgb/abci.go.EndBlocker:42
		if valset.Height < req.Height &&
			(!k.HasValsetRequest(sdk.UnwrapSDKContext(c), valset.Nonce+1) ||
				(k.HasValsetRequest(sdk.UnwrapSDKContext(c), valset.Nonce+1) &&
					k.GetValset(sdk.UnwrapSDKContext(c), valset.Nonce+1).Height >= req.Height)) {
			vs, err := types.CopyValset(valset)
			if err != nil {
				return nil, err
			}
			return &types.QueryLastValsetBeforeHeightResponse{Valset: vs}, nil
		}
	}

	return nil, sdkerrors.Wrap(sdkerrors.ErrNotFound, "last valset request before height not found")
}
