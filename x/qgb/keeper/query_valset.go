package keeper

import (
	"context"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

//// TODO add unit tests for all of these requests
//// LastValsetRequests queries the LastValsetRequests of the qgb module
//func (k Keeper) LastValsetRequests(
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
func (k Keeper) LastValsetBeforeNonce(
	c context.Context,
	req *types.QueryLastValsetBeforeNonceRequest,
) (*types.QueryLastValsetBeforeNonceResponse, error) {
	// starting at 1 because the current nonce can be a valset
	// and we need the previous one.
	for i := uint64(1); i <= req.Nonce; i++ {
		at := k.GetAttestationByNonce(sdk.UnwrapSDKContext(c), req.Nonce-i)
		if at.Type() == types.ValsetRequestType {
			valset, ok := at.(*types.Valset)
			if !ok {
				return nil, sdkerrors.Wrap(types.ErrAttestationNotValsetRequest, "couldn't cast attestation to valset")
			}
			return &types.QueryLastValsetBeforeNonceResponse{Valset: valset}, nil
		}
	}

	return nil, sdkerrors.Wrap(sdkerrors.ErrNotFound, "last valset request before height not found")
}
