package version

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	coretypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

// EndBlocker returns the version of the app at a given height.
func EndBlocker(ctx sdk.Context, keeper Keeper, resp abci.ResponseEndBlock) abci.ResponseEndBlock {
	newAppVersion := keeper.GetVersion(ctx)
	if ctx.BlockHeader().Version.App != newAppVersion {
		resp.ConsensusParamUpdates.Version = &coretypes.VersionParams{
			AppVersion: newAppVersion,
		}
	}
	return resp
}
