package signal_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/x/signal/types"
	"github.com/stretchr/testify/require"

	tmlog "github.com/cometbft/cometbft/libs/log"
	tmtypes "github.com/cometbft/cometbft/proto/tendermint/types"
	tmversion "github.com/cometbft/cometbft/proto/tendermint/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TestUpgradeIntegration uses the real application including the upgrade keeper (and staking keeper). It
// simulates an upgrade scenario with a single validator which signals for the version change, checks the quorum
// has been reached and then calls TryUpgrade, asserting that the upgrade module returns the new app version
func TestUpgradeIntegration(t *testing.T) {
	cp := app.DefaultConsensusParams()
	cp.Version.AppVersion = v2.Version
	app, _ := testutil.SetupTestAppWithGenesisValSet(cp)
	ctx := sdk.NewContext(app.CommitMultiStore(), tmtypes.Header{
		Version: tmversion.Consensus{
			App: v2.Version,
		},
		ChainID: appconsts.TestChainID,
	}, false, tmlog.NewNopLogger())
	goCtx := sdk.WrapSDKContext(ctx)
	ctx = sdk.UnwrapSDKContext(goCtx)

	res, err := app.SignalKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 3,
	})
	require.NoError(t, err)
	require.EqualValues(t, 0, res.VotingPower)

	validators := app.StakingKeeper.GetAllValidators(ctx)
	valAddr, err := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	require.NoError(t, err)

	_, err = app.SignalKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: valAddr.String(),
		Version:          3,
	})
	require.NoError(t, err)

	res, err = app.SignalKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 3,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, res.VotingPower)
	require.EqualValues(t, 1, res.ThresholdPower)
	require.EqualValues(t, 1, res.TotalVotingPower)

	_, err = app.SignalKeeper.TryUpgrade(ctx, nil)
	require.NoError(t, err)

	// Verify that if a user queries the version tally, it still works after a
	// successful try upgrade.
	res, err = app.SignalKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
		Version: 3,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, res.VotingPower)
	require.EqualValues(t, 1, res.ThresholdPower)
	require.EqualValues(t, 1, res.TotalVotingPower)

	// Verify that if a subsequent call to TryUpgrade is made, it returns an
	// error because an upgrade is already pending.
	_, err = app.SignalKeeper.TryUpgrade(ctx, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUpgradePending)

	// Verify that if a validator tries to change their signal version, it
	// returns an error because an upgrade is pending.
	_, err = app.SignalKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: valAddr.String(),
		Version:          4,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUpgradePending)

	shouldUpgrade, version := app.SignalKeeper.ShouldUpgrade(ctx)
	require.False(t, shouldUpgrade)
	require.EqualValues(t, 0, version)

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + appconsts.UpgradeHeightDelay(appconsts.TestChainID, version))

	shouldUpgrade, version = app.SignalKeeper.ShouldUpgrade(ctx)
	require.True(t, shouldUpgrade)
	require.EqualValues(t, 3, version)
}
