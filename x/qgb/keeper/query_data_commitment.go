package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const maxDataCommitmentRequestsReturned = 5

func (k Keeper) DataCommitmentRequestByNonce(
	ctx context.Context,
	request *types.QueryDataCommitmentRequestByNonceRequest,
) (*types.QueryDataCommitmentRequestByNonceResponse, error) {
	return &types.QueryDataCommitmentRequestByNonceResponse{
		Commitment: k.GetDataCommitment(
			sdk.UnwrapSDKContext(ctx),
			request.Nonce,
		),
	}, nil
}

func (k Keeper) LastDataCommitmentRequests(
	ctx context.Context,
	request *types.QueryLastDataCommitmentRequestsRequest,
) (*types.QueryLastDataCommitmentRequestsResponse, error) {
	dcReq := k.GetDataCommitments(sdk.UnwrapSDKContext(ctx))
	dcReqLen := len(dcReq)
	retLen := 0
	if dcReqLen < maxDataCommitmentRequestsReturned {
		retLen = dcReqLen
	} else {
		retLen = maxDataCommitmentRequestsReturned
	}
	return &types.QueryLastDataCommitmentRequestsResponse{Commitments: dcReq[0:retLen]}, nil
}

func (k Keeper) LatestDataCommitmentNonce(
	ctx context.Context,
	request *types.QueryLatestDataCommitmentNonceRequest,
) (*types.QueryLatestDataCommitmentNonceResponse, error) {
	return &types.QueryLatestDataCommitmentNonceResponse{
		Nonce: k.GetLatestDataCommitmentNonce(sdk.UnwrapSDKContext(ctx)),
	}, nil
}
