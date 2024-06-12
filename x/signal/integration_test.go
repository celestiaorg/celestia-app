package signal_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/x/signal/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
)

// TestUpgradeIntegration uses the real application including the upgrade keeper (and staking keeper). It
// simulates an upgrade scenario with a single validator which signals for the version change, checks the quorum
// has been reached and then calls TryUpgrade, asserting that the upgrade module returns the new app version
func TestUpgradeIntegration(t *testing.T) {
	app, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := sdk.NewContext(app.CommitMultiStore(), tmtypes.Header{
		Version: tmversion.Consensus{
			App: 1,
		},
	}, false, tmlog.NewNopLogger())
	goCtx := sdk.WrapSDKContext(ctx)
	ctx = sdk.UnwrapSDKContext(goCtx)

	res, err := app.SignalKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 0, res.VotingPower)

	validators := app.StakingKeeper.GetAllValidators(ctx)
	valAddr, err := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	require.NoError(t, err)

	_, err = app.SignalKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: valAddr.String(),
		Version:          2,
	})
	require.NoError(t, err)

	res, err = app.SignalKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, res.VotingPower)
	require.EqualValues(t, 1, res.ThresholdPower)
	require.EqualValues(t, 1, res.TotalVotingPower)

	_, err = app.SignalKeeper.TryUpgrade(ctx, nil)
	require.NoError(t, err)

	shouldUpgrade, version := app.SignalKeeper.ShouldUpgrade(ctx)
	require.False(t, shouldUpgrade)
	require.EqualValues(t, 0, version)

	// advance the block height by 48 hours worth of 12 second blocks.
	upgradeHeightDelay := int64(48 * 60 * 60 / 12)
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + upgradeHeightDelay)

	shouldUpgrade, version = app.SignalKeeper.ShouldUpgrade(ctx)
	require.True(t, shouldUpgrade)
	require.EqualValues(t, 2, version)
}
