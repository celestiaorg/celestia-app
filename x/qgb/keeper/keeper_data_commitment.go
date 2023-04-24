package keeper

import (
	"fmt"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TODO add unit tests for all the keepers

// GetCurrentDataCommitment creates the latest data commitment at current height according to
// the data commitment window specified
func (k Keeper) GetCurrentDataCommitment(ctx sdk.Context) (types.DataCommitment, error) {
	beginBlock := uint64(ctx.BlockHeight()) - k.GetDataCommitmentWindowParam(ctx) + 1
	// for a data commitment window of 400, the ranges will be: 1-400;401-800;801-1200
	endBlock := uint64(ctx.BlockHeight())

	if !k.CheckLatestAttestationNonce(ctx) {
		return types.DataCommitment{}, types.ErrLatestAttestationNonceStillNotInitialized
	}
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
		return types.DataCommitment{}, errors.Wrap(
			types.ErrDataCommitmentNotGenerated,
			fmt.Sprintf(
				"Last height %d < %d",
				lastDC.EndBlock,
				height,
			),
		)
	}
	if !k.CheckLatestAttestationNonce(ctx) {
		return types.DataCommitment{}, types.ErrLatestAttestationNonceStillNotInitialized
	}
	latestNonce := k.GetLatestAttestationNonce(ctx)
	for i := latestNonce; i > 0; i-- {
		// TODO better search
		att, found, err := k.GetAttestationByNonce(ctx, i)
		if err != nil {
			return types.DataCommitment{}, err
		}
		if !found {
			return types.DataCommitment{}, errors.Wrap(types.ErrAttestationNotFound, fmt.Sprintf("nonce %d", i))
		}
		dcc, ok := att.(*types.DataCommitment)
		if !ok {
			continue
		}
		if dcc.BeginBlock <= height && dcc.EndBlock >= height {
			return *dcc, nil
		}
	}
	return types.DataCommitment{}, errors.Wrap(types.ErrDataCommitmentNotFound, "data commitment for height not found")
}

// GetLastDataCommitment returns the last data commitment.
func (k Keeper) GetLastDataCommitment(ctx sdk.Context) (types.DataCommitment, error) {
	if !k.CheckLatestAttestationNonce(ctx) {
		return types.DataCommitment{}, types.ErrLatestAttestationNonceStillNotInitialized
	}
	latestNonce := k.GetLatestAttestationNonce(ctx)
	for i := uint64(0); i < latestNonce; i++ {
		att, found, err := k.GetAttestationByNonce(ctx, latestNonce-i)
		if err != nil {
			return types.DataCommitment{}, err
		}
		if !found {
			return types.DataCommitment{}, errors.Wrapf(types.ErrAttestationNotFound, fmt.Sprintf("nonce %d", latestNonce-i))
		}
		dcc, ok := att.(*types.DataCommitment)
		if !ok {
			continue
		}
		return *dcc, nil
	}
	return types.DataCommitment{}, types.ErrDataCommitmentNotFound
}
