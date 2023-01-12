package keeper

import (
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
