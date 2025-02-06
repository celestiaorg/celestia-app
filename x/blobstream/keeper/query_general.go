package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/v4/x/blobstream/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// LatestUnbondingHeight queries the latest unbonding height.
func (k Keeper) LatestUnbondingHeight(
	c context.Context,
	_ *types.QueryLatestUnbondingHeightRequest,
) (*types.QueryLatestUnbondingHeightResponse, error) {
	return &types.QueryLatestUnbondingHeightResponse{
		Height: k.GetLatestUnBondingBlockHeight(sdk.UnwrapSDKContext(c)),
	}, nil
}

// EarliestAttestationNonce queries the earliest attestation nonce.
func (k Keeper) EarliestAttestationNonce(
	c context.Context,
	_ *types.QueryEarliestAttestationNonceRequest,
) (*types.QueryEarliestAttestationNonceResponse, error) {
	return &types.QueryEarliestAttestationNonceResponse{
		Nonce: k.GetEarliestAvailableAttestationNonce(sdk.UnwrapSDKContext(c)),
	}, nil
}

func (k Keeper) Params(c context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params := k.GetParams(sdk.UnwrapSDKContext(c))
	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}

// EVMAddress tries to find the associated EVM address for a given validator address. If
// none is found, an empty address is returned
func (k Keeper) EVMAddress(goCtx context.Context, req *types.QueryEVMAddressRequest) (*types.QueryEVMAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddress)
	if err != nil {
		return nil, err
	}
	evmAddr, exists := k.GetEVMAddress(ctx, valAddr)
	if !exists {
		return &types.QueryEVMAddressResponse{}, nil
	}
	return &types.QueryEVMAddressResponse{
		EvmAddress: evmAddr.Hex(),
	}, nil
}
