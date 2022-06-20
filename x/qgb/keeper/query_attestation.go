package keeper

import (
	"context"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const maxDataCommitmentRequestsReturned = 5

func (k Keeper) AttestationRequestByNonce(
	ctx context.Context,
	request *types.QueryAttestationRequestByNonceRequest,
) (*types.QueryAttestationRequestByNonceResponse, error) {
	val, err := codectypes.NewAnyWithValue(
		k.GetAttestationByNonce(
			sdk.UnwrapSDKContext(ctx),
			request.Nonce,
		))
	if err != nil {
		return nil, err
	}
	return &types.QueryAttestationRequestByNonceResponse{
		Attestation: val,
	}, nil
}

func (k Keeper) LatestAttestationNonce(
	ctx context.Context,
	request *types.QueryLatestAttestationNonceRequest,
) (*types.QueryLatestAttestationNonceResponse, error) {
	return &types.QueryLatestAttestationNonceResponse{
		Nonce: k.GetLatestAttestationNonce(sdk.UnwrapSDKContext(ctx)),
	}, nil
}
