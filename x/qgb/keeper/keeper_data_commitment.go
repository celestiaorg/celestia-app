package keeper

import (
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TODO add unit tests for all the keepers

// GetCurrentDataCommitment creates latest data commitment at current height according to
// the data commitment window specified
func (k Keeper) GetCurrentDataCommitment(ctx sdk.Context) (types.DataCommitment, error) {
	beginBlock := uint64(ctx.BlockHeight()) - k.GetDataCommitmentWindowParam(ctx)
	endBlock := uint64(ctx.BlockHeight())
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
