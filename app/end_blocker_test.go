package app_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"cosmossdk.io/core/header"
	"cosmossdk.io/log"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	signaltypes "github.com/celestiaorg/celestia-app/v11/x/signal/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestEndBlockerUpgrade(t *testing.T) {
	current := appconsts.Version
	const upgradeHeight = int64(10)

	t.Run("no pending upgrade", func(t *testing.T) {
		testApp, home := newEndBlockerTestApp(t)
		ctx := testApp.NewUncachedContext(false, cmtproto.Header{Height: upgradeHeight})

		res, err := testApp.EndBlocker(ctx)
		require.NoError(t, err)
		require.Equal(t, testApp.TimeoutInfo(current), res.TimeoutInfo)
		requireNoUpgradeScheduled(t, testApp, ctx, home, current)
	})

	// A pending upgrade that does not raise the app version is cleared at its
	// activation height, so it can not block signalling (PROTOCO-3757).
	tests := []struct {
		name        string
		appVersion  uint64
		height      int64
		wantPending bool
	}{
		{name: "before activation height", appVersion: current, height: upgradeHeight - 1, wantPending: true},
		{name: "equal version", appVersion: current + 1, height: upgradeHeight},
		{name: "lower version", appVersion: current + 2, height: upgradeHeight},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testApp, home := newEndBlockerTestApp(t)
			ctx := testApp.NewUncachedContext(false, cmtproto.Header{Height: tc.height})
			setPendingUpgrade(t, testApp, ctx, current+1, upgradeHeight)
			require.NoError(t, testApp.SetAppVersion(ctx, tc.appVersion))

			_, err := testApp.EndBlocker(ctx)
			require.NoError(t, err)
			requireNoUpgradeScheduled(t, testApp, ctx, home, tc.appVersion)
			require.Equal(t, tc.wantPending, testApp.SignalKeeper.IsUpgradePending(ctx))
		})
	}

	t.Run("higher version at activation height", func(t *testing.T) {
		testApp, home := newEndBlockerTestApp(t)
		ctx := testApp.NewUncachedContext(false, cmtproto.Header{})
		signalAndTryUpgrade(t, testApp, ctx, current+1)
		upgrade, err := testApp.SignalKeeper.GetUpgrade(ctx, nil)
		require.NoError(t, err)
		height := upgrade.Upgrade.UpgradeHeight
		ctx = ctx.WithBlockHeight(height).WithHeaderInfo(header.Info{Height: height})

		res, err := testApp.EndBlocker(ctx)
		require.NoError(t, err)
		require.Equal(t, testApp.TimeoutInfo(current+1), res.TimeoutInfo)

		appVersion, err := testApp.AppVersion(ctx)
		require.NoError(t, err)
		require.Equal(t, current+1, appVersion)

		want := upgradetypes.Plan{Name: fmt.Sprintf("v%d", current+1), Height: height + 1}
		plan, err := testApp.UpgradeKeeper.GetUpgradePlan(ctx)
		require.NoError(t, err)
		require.Equal(t, want, plan)
		require.NoFileExists(t, upgradeInfoPath(home), "written at Commit, not in EndBlocker")

		require.False(t, testApp.SignalKeeper.IsUpgradePending(ctx))
		tally, err := testApp.SignalKeeper.VersionTally(ctx, &signaltypes.QueryVersionTallyRequest{Version: current + 1})
		require.NoError(t, err)
		require.Zero(t, tally.VotingPower)
		missing, err := testApp.SignalKeeper.GetMissingValidators(ctx, &signaltypes.QueryGetMissingValidatorsRequest{Version: current + 1})
		require.NoError(t, err)
		require.Len(t, missing.MissingValidators, 1)
	})

	t.Run("app version read failure", func(t *testing.T) {
		testApp, _ := newEndBlockerTestApp(t)
		ctx := testApp.NewUncachedContext(false, cmtproto.Header{Height: upgradeHeight})
		require.NoError(t, testApp.ConsensusKeeper.ParamsStore.Remove(ctx))

		_, err := testApp.EndBlocker(ctx)
		require.Error(t, err)
	})
}

// TestEndBlockerUpgradeFailureIsNotCommitted checks that a failed upgrade in
// EndBlocker fails FinalizeBlock and leaves committed state unchanged.
func TestEndBlockerUpgradeFailureIsNotCommitted(t *testing.T) {
	current := appconsts.Version
	testApp, _ := newEndBlockerTestApp(t)
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{})
	lastHeight := testApp.LastBlockHeight()
	// The plan height would be before the current block.
	setPendingUpgrade(t, testApp, ctx, current+1, lastHeight-1)

	_, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{Height: lastHeight + 1, Time: util.GenesisTime})
	require.ErrorContains(t, err, "failed to schedule upgrade")

	require.Equal(t, lastHeight, testApp.LastBlockHeight())
	ctx = testApp.NewUncachedContext(false, cmtproto.Header{})
	require.True(t, testApp.SignalKeeper.IsUpgradePending(ctx))
	appVersion, err := testApp.AppVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, current, appVersion)
	_, err = testApp.UpgradeKeeper.GetUpgradePlan(ctx)
	require.ErrorIs(t, err, upgradetypes.ErrNoUpgradePlanFound)
}

// TestUpgradeInfoWrittenAtCommit checks that upgrade-info.json appears only
// when the block that schedules the upgrade is committed, including when
// FinalizeBlock is served by optimistic execution.
func TestUpgradeInfoWrittenAtCommit(t *testing.T) {
	current := appconsts.Version
	for _, tc := range []struct {
		name string
		// run executes height h up to, not including, Commit.
		run func(t *testing.T, testApp *app.App, h int64)
	}{
		{name: "finalize block", run: func(t *testing.T, testApp *app.App, h int64) {
			_, err := testApp.FinalizeBlock(stateFixBlock(h))
			require.NoError(t, err)
		}},
		{name: "optimistic execution served", run: func(t *testing.T, testApp *app.App, h int64) {
			req := proposeAndProcess(t, testApp, h, []byte("block-a"))
			_, err := testApp.FinalizeBlock(req)
			require.NoError(t, err)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testApp, home := newOptimisticTestApp(t)
			h := testApp.LastBlockHeight() + 1
			setPendingUpgrade(t, testApp, testApp.NewUncachedContext(false, cmtproto.Header{}), current+1, h)

			tc.run(t, testApp, h)
			require.NoFileExists(t, upgradeInfoPath(home), "upgrade info published before Commit")

			_, err := testApp.Commit()
			require.NoError(t, err)
			want := upgradetypes.Plan{Name: fmt.Sprintf("v%d", current+1), Height: h + 1}
			require.Equal(t, want, readUpgradeInfo(t, home))
		})
	}
}

// TestUpgradeInfoFromDiscardedRunNotWritten checks that a plan recorded by a
// discarded EndBlocker run is not written by the next committed block.
func TestUpgradeInfoFromDiscardedRunNotWritten(t *testing.T) {
	testApp, home := newEndBlockerTestApp(t)
	h := testApp.LastBlockHeight() + 1
	discarded, _ := testApp.NewUncachedContext(false, cmtproto.Header{Height: h}).CacheContext()
	discarded = discarded.WithHeaderInfo(header.Info{Height: h})
	setPendingUpgrade(t, testApp, discarded, appconsts.Version+1, h)
	_, err := testApp.EndBlocker(discarded)
	require.NoError(t, err)
	_, err = testApp.UpgradeKeeper.GetUpgradePlan(discarded)
	require.NoError(t, err, "discarded run must schedule an upgrade")

	_, err = testApp.FinalizeBlock(stateFixBlock(h))
	require.NoError(t, err)
	_, err = testApp.Commit()
	require.NoError(t, err)
	require.NoFileExists(t, upgradeInfoPath(home))
}

// TestUpgradeInfoWriteFailureIsNotCommitted checks that a failed upgrade info
// write fails Commit before the block is committed.
func TestUpgradeInfoWriteFailureIsNotCommitted(t *testing.T) {
	testApp, home := newEndBlockerTestApp(t)
	lastHeight := testApp.LastBlockHeight()
	setPendingUpgrade(t, testApp, testApp.NewUncachedContext(false, cmtproto.Header{}), appconsts.Version+1, lastHeight+1)
	// A directory in place of the upgrade info file makes the write fail.
	require.NoError(t, os.MkdirAll(upgradeInfoPath(home), 0o700))

	_, err := testApp.FinalizeBlock(stateFixBlock(lastHeight + 1))
	require.NoError(t, err)
	_, err = testApp.Commit()
	require.ErrorContains(t, err, "failed to dump upgrade info to disk")
	require.Equal(t, lastHeight, testApp.LastBlockHeight())
}

// newOptimisticTestApp is newEndBlockerTestApp with optimistic execution on.
func newOptimisticTestApp(t *testing.T) (*app.App, string) {
	t.Helper()
	home := setNodeHome(t)
	testApp := app.New(log.NewNopLogger(), tmdb.NewMemDB(), nil, 0, 0, util.EmptyAppOptions{},
		baseapp.SetChainID(util.ChainID), baseapp.SetOptimisticExecution())
	genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp)
	testApp = util.InitialiseTestAppWithGenesis(testApp, app.DefaultConsensusParams(), genesisState)
	finalizeAndCommit(t, testApp)
	return testApp, home
}

// proposeAndProcess runs PrepareProposal and an accepted ProcessProposal for
// height h, which starts optimistic execution, and returns the matching block.
func proposeAndProcess(t *testing.T, testApp *app.App, h int64, hash []byte) *abci.RequestFinalizeBlock {
	t.Helper()
	req := stateFixBlock(h)
	req.Hash = hash
	prop, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{Height: h, Time: req.Time})
	require.NoError(t, err)
	pp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
		Txs: prop.Txs, SquareSize: prop.SquareSize, DataRootHash: prop.DataRootHash,
		Hash: hash, Height: h, Time: req.Time, Header: req.Header,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, pp.Status)
	req.Txs = prop.Txs
	return req
}

// TestNewAppWithPendingV11Upgrade checks that a node restarted at the v11
// upgrade height loads its existing stores. The v11 upgrade adds no stores, so
// it must not install an upgrade store loader.
func TestNewAppWithPendingV11Upgrade(t *testing.T) {
	home := setNodeHome(t)
	db := tmdb.NewMemDB()
	testApp := app.New(log.NewNopLogger(), db, nil, 0, 0, util.EmptyAppOptions{}, baseapp.SetChainID(util.ChainID))
	genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp)
	testApp = util.InitialiseTestAppWithGenesis(testApp, app.DefaultConsensusParams(), genesisState)
	finalizeAndCommit(t, testApp)

	height := testApp.LastBlockHeight() + 1
	plan, err := json.Marshal(upgradetypes.Plan{Name: fmt.Sprintf("v%d", appconsts.Version), Height: height})
	require.NoError(t, err)
	writeUpgradeInfo(t, home, plan)

	require.NotPanics(t, func() {
		app.New(log.NewNopLogger(), db, nil, 0, 0, util.EmptyAppOptions{}, baseapp.SetChainID(util.ChainID))
	})
}

// newEndBlockerTestApp returns an initialised app whose upgrade info is
// written to a temporary home directory.
func newEndBlockerTestApp(t *testing.T) (*app.App, string) {
	t.Helper()
	home := setNodeHome(t)
	testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	return testApp, home
}

// setNodeHome points app.NodeHome at a temporary directory for the test.
func setNodeHome(t *testing.T) string {
	t.Helper()
	original := app.NodeHome
	app.NodeHome = t.TempDir()
	t.Cleanup(func() { app.NodeHome = original })
	return app.NodeHome
}

// setPendingUpgrade stores a pending signal upgrade directly.
func setPendingUpgrade(t *testing.T, testApp *app.App, ctx sdk.Context, appVersion uint64, height int64) {
	t.Helper()
	upgrade := signaltypes.Upgrade{AppVersion: appVersion, UpgradeHeight: height}
	bz, err := testApp.AppCodec().Marshal(&upgrade)
	require.NoError(t, err)
	ctx.KVStore(testApp.GetKey(signaltypes.StoreKey)).Set(signaltypes.UpgradeKey, bz)
}

// signalAndTryUpgrade signals for appVersion from the only validator and
// tallies the vote.
func signalAndTryUpgrade(t *testing.T, testApp *app.App, ctx sdk.Context, appVersion uint64) {
	t.Helper()
	validators, err := testApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	valAddr := validators[0].OperatorAddress
	_, err = testApp.SignalKeeper.SignalVersion(ctx, &signaltypes.MsgSignalVersion{ValidatorAddress: valAddr, Version: appVersion})
	require.NoError(t, err)
	_, err = testApp.SignalKeeper.TryUpgrade(ctx, &signaltypes.MsgTryUpgrade{Signer: valAddr})
	require.NoError(t, err)
	require.True(t, testApp.SignalKeeper.IsUpgradePending(ctx))
}

func requireNoUpgradeScheduled(t *testing.T, testApp *app.App, ctx sdk.Context, home string, wantVersion uint64) {
	t.Helper()
	appVersion, err := testApp.AppVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, wantVersion, appVersion)
	_, err = testApp.UpgradeKeeper.GetUpgradePlan(ctx)
	require.ErrorIs(t, err, upgradetypes.ErrNoUpgradePlanFound)
	require.NoFileExists(t, upgradeInfoPath(home))
}

func upgradeInfoPath(home string) string {
	return filepath.Join(home, "data", upgradetypes.UpgradeInfoFilename)
}

func readUpgradeInfo(t *testing.T, home string) upgradetypes.Plan {
	t.Helper()
	bz, err := os.ReadFile(upgradeInfoPath(home))
	require.NoError(t, err)
	var plan upgradetypes.Plan
	require.NoError(t, json.Unmarshal(bz, &plan))
	return plan
}

func writeUpgradeInfo(t *testing.T, home string, bz []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(upgradeInfoPath(home)), 0o700))
	require.NoError(t, os.WriteFile(upgradeInfoPath(home), bz, 0o600))
}
