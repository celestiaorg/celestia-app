package keeper

import (
	"fmt"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v4/x/blobstream/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TODO add unit tests for all the keepers

// NextDataCommitment returns the next data commitment that can be written to
// state.
func (k Keeper) NextDataCommitment(ctx sdk.Context) (types.DataCommitment, error) {
	if !k.CheckLatestAttestationNonce(ctx) {
		return types.DataCommitment{}, types.ErrLatestAttestationNonceStillNotInitialized
	}
	nonce := k.GetLatestAttestationNonce(ctx) + 1
	dcWindow := k.GetDataCommitmentWindowParam(ctx)
	hasDC, err := k.HasDataCommitmentInStore(ctx)
	if err != nil {
		return types.DataCommitment{}, err
	}
	// for a data commitment window of 400, the ranges will be: [1-401), [401-801), [801-1201)
	var beginBlock, endBlock uint64
	if hasDC {
		latestDCC, err := k.GetLatestDataCommitment(ctx)
		if err != nil {
			return types.DataCommitment{}, err
		}
		beginBlock = latestDCC.EndBlock
		endBlock = latestDCC.EndBlock + dcWindow
	} else {
		// only for the first data commitment range, which is: [1, data commitment window + 1)
		beginBlock = 1
		endBlock = dcWindow + 1
	}

	dataCommitment := types.NewDataCommitment(nonce, beginBlock, endBlock, ctx.BlockTime())
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
	latestDC, err := k.GetLatestDataCommitment(ctx)
	if err != nil {
		return types.DataCommitment{}, err
	}
	if latestDC.EndBlock < height {
		return types.DataCommitment{}, errors.Wrap(
			types.ErrDataCommitmentNotGenerated,
			fmt.Sprintf(
				"Latest height %d < %d",
				latestDC.EndBlock,
				height,
			),
		)
	}
	if !k.CheckLatestAttestationNonce(ctx) {
		return types.DataCommitment{}, types.ErrLatestAttestationNonceStillNotInitialized
	}
	if !k.CheckEarliestAvailableAttestationNonce(ctx) {
		return types.DataCommitment{}, types.ErrEarliestAvailableNonceStillNotInitialized
	}
	latestNonce := k.GetLatestAttestationNonce(ctx)
	earliestAvailableNonce := k.GetEarliestAvailableAttestationNonce(ctx)
	for i := latestNonce; i >= earliestAvailableNonce; i-- {
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
		if dcc.BeginBlock <= height && dcc.EndBlock > height {
			return *dcc, nil
		}
	}
	return types.DataCommitment{}, errors.Wrap(types.ErrDataCommitmentNotFound, "data commitment for height not found or was pruned")
}

// GetLatestDataCommitment returns the latest data commitment.
func (k Keeper) GetLatestDataCommitment(ctx sdk.Context) (types.DataCommitment, error) {
	if !k.CheckLatestAttestationNonce(ctx) {
		return types.DataCommitment{}, types.ErrLatestAttestationNonceStillNotInitialized
	}
	if !k.CheckEarliestAvailableAttestationNonce(ctx) {
		return types.DataCommitment{}, types.ErrEarliestAvailableNonceStillNotInitialized
	}
	latestNonce := k.GetLatestAttestationNonce(ctx)
	earliestAvailableNonce := k.GetEarliestAvailableAttestationNonce(ctx)
	for i := latestNonce; i >= earliestAvailableNonce; i-- {
		att, found, err := k.GetAttestationByNonce(ctx, i)
		if err != nil {
			return types.DataCommitment{}, err
		}
		if !found {
			return types.DataCommitment{}, errors.Wrapf(types.ErrAttestationNotFound, "nonce %d", i)
		}
		dcc, ok := att.(*types.DataCommitment)
		if !ok {
			continue
		}
		return *dcc, nil
	}
	return types.DataCommitment{}, types.ErrDataCommitmentNotFound
}

// HasDataCommitmentInStore returns true if the store has at least one data commitment.
func (k Keeper) HasDataCommitmentInStore(ctx sdk.Context) (bool, error) {
	if !k.CheckLatestAttestationNonce(ctx) {
		return false, nil
	}
	if !k.CheckEarliestAvailableAttestationNonce(ctx) {
		return false, types.ErrEarliestAvailableNonceStillNotInitialized
	}
	latestNonce := k.GetLatestAttestationNonce(ctx)
	earliestAvailableNonce := k.GetEarliestAvailableAttestationNonce(ctx)
	for i := earliestAvailableNonce; i <= latestNonce; i++ {
		att, found, err := k.GetAttestationByNonce(ctx, i)
		if err != nil {
			return false, err
		}
		if !found {
			return false, errors.Wrapf(types.ErrAttestationNotFound, "nonce %d", i)
		}
		_, ok := att.(*types.DataCommitment)
		if !ok {
			continue
		}
		return true, nil
	}
	return false, nil
}
