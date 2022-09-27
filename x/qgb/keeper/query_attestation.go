package keeper

import (
	"context"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k Keeper) AttestationRequestByNonce(
	ctx context.Context,
	request *types.QueryAttestationRequestByNonceRequest,
) (*types.QueryAttestationRequestByNonceResponse, error) {
	attestation, found, err := k.GetAttestationByNonce(
		sdk.UnwrapSDKContext(ctx),
		request.Nonce,
	)
	if err != nil {
		return nil, err
	}
	if !found {
		return &types.QueryAttestationRequestByNonceResponse{}, types.ErrAttestationNotFound
	}
	val, err := codectypes.NewAnyWithValue(attestation)
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
