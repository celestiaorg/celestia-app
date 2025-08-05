package app_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/log"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/test/util"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
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
	t.Run("ICA host params should have an allowlist of messages before and after ApplyUpgrade", func(t *testing.T) {
		testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
		require.True(t, testApp.UpgradeKeeper.HasHandler("v6"))
		plan := upgradetypes.Plan{
			Name:   "v6",
			Time:   time.Now(),
			Height: 1,
			Info:   "info",
		}

		ctx := testApp.NewContext(false)
		params := testApp.ICAHostKeeper.GetParams(ctx)
		require.True(t, params.HostEnabled)
		require.Equal(t, params.AllowMessages, app.IcaAllowMessages())
		fmt.Printf("params before upgrade: %+v\n", params)

		err := testApp.UpgradeKeeper.ApplyUpgrade(ctx, plan)
		require.NoError(t, err)

		ctx = testApp.NewContext(false)
		params = testApp.ICAHostKeeper.GetParams(ctx)
		require.True(t, params.HostEnabled)
		require.Equal(t, params.AllowMessages, app.IcaAllowMessages())
		fmt.Printf("params after upgrade: %+v\n", params)
	})
}
