package keeper

import (
	"context"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"

	"github.com/celestiaorg/celestia-app/v2/x/blobstream/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k Keeper) AttestationRequestByNonce(
	ctx context.Context,
	request *types.QueryAttestationRequestByNonceRequest,
) (*types.QueryAttestationRequestByNonceResponse, error) {
	unwrappedCtx := sdk.UnwrapSDKContext(ctx)
	if latestAttestationNonce := k.GetLatestAttestationNonce(unwrappedCtx); latestAttestationNonce < request.Nonce {
		return nil, types.ErrNonceHigherThanLatestAttestationNonce
	}

	attestation, found, err := k.GetAttestationByNonce(
		unwrappedCtx,
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
	_ *types.QueryLatestAttestationNonceRequest,
) (*types.QueryLatestAttestationNonceResponse, error) {
	if !k.CheckLatestAttestationNonce(sdk.UnwrapSDKContext(ctx)) {
		return nil, types.ErrLatestAttestationNonceStillNotInitialized
	}
	return &types.QueryLatestAttestationNonceResponse{
		Nonce: k.GetLatestAttestationNonce(sdk.UnwrapSDKContext(ctx)),
	}, nil
}
