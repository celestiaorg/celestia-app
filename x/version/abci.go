package version

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	coretypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

// EndBlocker will modify the app version if the current height is equal to
// a predefined height at which the app version should be changed.
func EndBlocker(ctx sdk.Context, keeper Keeper, resp abci.ResponseEndBlock) abci.ResponseEndBlock {
	newAppVersion := keeper.GetVersion(ctx)
	if ctx.BlockHeader().Version.App != newAppVersion {
		resp.ConsensusParamUpdates.Version = &coretypes.VersionParams{
			AppVersion: newAppVersion,
		}
		// set the version in the application to ensure that tendermint is
		// passed the correct value upon rebooting
		keeper.versionSetter.SetProtocolVersion(newAppVersion)
	}
	return resp
}
