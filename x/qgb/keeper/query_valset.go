package keeper

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

//// TODO add unit tests for all of these requests
//// LastValsetRequests queries the LastValsetRequests of the qgb module
// TODO uncomment and implement: can be used in tests
//func (k Keeper) LastValsetRequest(
//	c context.Context,
//	req *types.QueryLastValsetRequestsRequest) (*types.QueryLastValsetRequestsResponse, error) {
//	valReq := k.GetValsets(sdk.UnwrapSDKContext(c))
//	valReqLen := len(valReq)
//	retLen := 0
//	if valReqLen < maxValsetRequestsReturned {
//		retLen = valReqLen
//	} else {
//		retLen = maxValsetRequestsReturned
//	}
//	// TODO: check if we need the first ones or the last ones
//	return &types.QueryLastValsetRequestsResponse{Valsets: valReq[0:retLen]}, nil
//}

//
//// ValsetRequestByNonce queries the Valset request of the qgb module by nonce
//func (k Keeper) ValsetRequestByNonce(
//	c context.Context,
//	req *types.QueryValsetRequestByNonceRequest) (*types.QueryValsetRequestByNonceResponse, error) {
//	// TODO add test for this
//	return &types.QueryValsetRequestByNonceResponse{Valset: k.GetValset(
//		sdk.UnwrapSDKContext(c),
//		req.Nonce,
//	)}, nil
//}

// LastValsetBeforeHeight queries the last valset request before height
// TODO rename to LastValsetRequestBeforeNonce
func (k Keeper) LastValsetBeforeNonce(
	c context.Context,
	req *types.QueryLastValsetBeforeNonceRequest,
) (*types.QueryLastValsetBeforeNonceResponse, error) {
	vs, err := k.GetLastValsetBeforeNonce(sdk.UnwrapSDKContext(c), req.Nonce)
	if err != nil {
		return nil, err
	}
	return &types.QueryLastValsetBeforeNonceResponse{Valset: vs}, nil
}
