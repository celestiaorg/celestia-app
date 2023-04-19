package version

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

// AppVersion returns the version of the app at a given height.
func AppVersion(ctx sdk.Context, keeper Keeper, req abci.RequestEndBlock) (appVersion uint64) {
	return keeper.GetVersion(ctx, req.Height)
}
