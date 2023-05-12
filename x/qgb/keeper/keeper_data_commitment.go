package keeper

import (
	"fmt"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TODO add unit tests for all the keepers

// GetCurrentDataCommitment returns a data commitment for the current height.
// Assumes that this method is called when:
//
//	ctx.BlockHeight() != 0 && ctx.BlockHeight()%int64(k.GetDataCommitmentWindowParam(ctx)) == 0
func (k Keeper) GetCurrentDataCommitment(ctx sdk.Context) (types.DataCommitment, error) {
	if !k.CheckLatestAttestationNonce(ctx) {
		return types.DataCommitment{}, types.ErrLatestAttestationNonceStillNotInitialized
	}
	nonce := k.GetLatestAttestationNonce(ctx) + 1
	dcWindow := k.GetDataCommitmentWindowParam(ctx)
	// for a data commitment window of 400, the ranges will be: 1-400;401-800;801-1200
	var beginBlock, endBlock uint64
	if ctx.BlockHeight() == int64(dcWindow) {
		// only for the first data commitment range, which is: [1, data commitment window]
		beginBlock = 1
		endBlock = dcWindow
	} else {
		lastDCC, err := k.GetLastDataCommitment(ctx)
		if err != nil {
			return types.DataCommitment{}, err
		}
		beginBlock = lastDCC.EndBlock + 1
		endBlock = lastDCC.EndBlock + dcWindow
	}

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
