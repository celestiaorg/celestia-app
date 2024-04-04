package upgrade_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/x/upgrade/types"
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

	res, err := app.UpgradeKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 0, res.VotingPower)

	validators := app.StakingKeeper.GetAllValidators(ctx)
	valAddr, err := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	require.NoError(t, err)

	_, err = app.UpgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: valAddr.String(),
		Version:          2,
	})
	require.NoError(t, err)

	res, err = app.UpgradeKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, res.VotingPower)
	require.EqualValues(t, 1, res.ThresholdPower)
	require.EqualValues(t, 1, res.TotalVotingPower)

	_, err = app.UpgradeKeeper.TryUpgrade(ctx, nil)
	require.NoError(t, err)

	shouldUpgrade, version := app.UpgradeKeeper.ShouldUpgrade()
	require.True(t, shouldUpgrade)
	require.EqualValues(t, 2, version)
}
