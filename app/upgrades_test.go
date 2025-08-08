package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/test/util"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	"github.com/stretchr/testify/require"
)

func TestUpgrades(t *testing.T) {
	t.Run("app.New() should register a v6 upgrade handler", func(t *testing.T) {
		logger := log.NewNopLogger()
		db := tmdb.NewMemDB()
		traceStore := &NoopWriter{}
		timeoutCommit := time.Second
		appOptions := NoopAppOptions{}

		testApp := app.New(logger, db, traceStore, timeoutCommit, appOptions, baseapp.SetChainID(testfactory.ChainID))

		require.False(t, testApp.UpgradeKeeper.HasHandler("v5"))
		require.True(t, testApp.UpgradeKeeper.HasHandler("v6"))
	})
}

func TestApplyUpgrade(t *testing.T) {
	t.Run("apply upgrade should set ICA host params to an explicit allowlist of messages", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		consensusParams.Version.App = 5
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
		require.True(t, testApp.UpgradeKeeper.HasHandler("v6"))
		plan := upgradetypes.Plan{
			Name:   "v6",
			Time:   time.Now(),
			Height: 1,
			Info:   "info",
		}

		// Note: v5 didn't have the ICA module registered so no params were set
		// but this test explicitly sets the params to values to verify they get
		// overridden during ApplyUpgrade.
		allMessages := []string{"*"}
		ctx := testApp.NewContext(false)
		testApp.ICAHostKeeper.SetParams(ctx, icahosttypes.Params{
			HostEnabled:   false,
			AllowMessages: allMessages,
		})
		got := testApp.ICAHostKeeper.GetParams(ctx)
		require.False(t, got.HostEnabled)
		require.Equal(t, allMessages, got.AllowMessages)

		err := testApp.UpgradeKeeper.ApplyUpgrade(ctx, plan)
		require.NoError(t, err)

		ctx = testApp.NewContext(false)
		got = testApp.ICAHostKeeper.GetParams(ctx)
		require.True(t, got.HostEnabled)
		require.Equal(t, got.AllowMessages, app.IcaAllowMessages())
	})
}
