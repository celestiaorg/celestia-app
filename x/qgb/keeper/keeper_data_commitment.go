package keeper

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TODO add unit tests for all the keepers

// GetCurrentDataCommitment creates the latest data commitment at current height according to
// the data commitment window specified
func (k Keeper) GetCurrentDataCommitment(ctx sdk.Context) (types.DataCommitment, error) {
	beginBlock := uint64(ctx.BlockHeight()) - k.GetDataCommitmentWindowParam(ctx)
	// to avoid having overlapped ranges of data commitments such as: 0-400;400-800;800-1200
	// we will commit to the previous block height so that the ranges are as follows: 0-399;400-799;800-1199
	endBlock := uint64(ctx.BlockHeight()) - 1
	nonce := k.GetLatestAttestationNonce(ctx) + 1

	dataCommitment := types.NewDataCommitment(nonce, beginBlock, endBlock)
	return *dataCommitment, nil
}

func (k Keeper) GetDataCommitmentWindowParam(ctx sdk.Context) uint64 {
	resp, err := k.Params(sdk.WrapSDKContext(ctx), &types.QueryParamsRequest{})
	if err != nil {
		panic(err)
	}
	return resp.Params.DataCommitmentWindow
}

// GetDataCommitmentForHeight returns the attestation containing the provided height.
func (k Keeper) GetDataCommitmentForHeight(ctx sdk.Context, height uint64) (types.DataCommitment, error) {
	lastDC, err := k.GetLastDataCommitment(ctx)
	if err != nil {
		return types.DataCommitment{}, err
	}
	if lastDC.EndBlock < height {
		return types.DataCommitment{}, fmt.Errorf(
			"no data commitment has been generated for the provided height. Last height %d < %d",
			lastDC.EndBlock,
			height,
		)
	}
	latestNonce := k.GetLatestAttestationNonce(ctx)
	for i := uint64(0); i < latestNonce; i++ {
		// TODO better search
		att, found, err := k.GetAttestationByNonce(ctx, latestNonce-i)
		if err != nil {
			return types.DataCommitment{}, err
		}
		if !found {
			return types.DataCommitment{}, fmt.Errorf("couldn't find attestation with nonce %d", latestNonce-i)
		}
		dcc, ok := att.(*types.DataCommitment)
		if !ok {
			continue
		}
		if dcc.BeginBlock <= height && dcc.EndBlock >= height {
			return *dcc, nil
		}
	}
	return types.DataCommitment{}, fmt.Errorf("data commitment for height not found")
}

// GetLastDataCommitment returns the last data commitment.
func (k Keeper) GetLastDataCommitment(ctx sdk.Context) (types.DataCommitment, error) {
	latestNonce := k.GetLatestAttestationNonce(ctx)
	for i := uint64(0); i < latestNonce; i++ {
		att, found, err := k.GetAttestationByNonce(ctx, latestNonce-i)
		if err != nil {
			return types.DataCommitment{}, err
		}
		if !found {
			return types.DataCommitment{}, fmt.Errorf("couldn't find attestation with nonce %d", latestNonce-i)
		}
		dcc, ok := att.(*types.DataCommitment)
		if !ok {
			continue
		}
		return *dcc, nil
	}
	return types.DataCommitment{}, fmt.Errorf("last data commitment not found")
}
