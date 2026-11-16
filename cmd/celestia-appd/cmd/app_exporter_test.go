package cmd

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestAppExporterHeight(t *testing.T) {
	db := newExportDB(t, 3)

	exported, err := exportFrom(db, -1, false)
	require.NoError(t, err)
	require.Equal(t, int64(4), exported.Height)

	exported, err = exportFrom(db, 2, false)
	require.NoError(t, err)
	require.Equal(t, int64(3), exported.Height)

	_, err = exportFrom(db, 10, false)
	require.Error(t, err)
}

func TestAppExporterRefusesMissingCommitTimestamp(t *testing.T) {
	db := newExportDB(t, 3)

	// Snapshot restore writes commit info without a timestamp.
	key := []byte("s/3")
	bz, err := db.Get(key)
	require.NoError(t, err)
	var info storetypes.CommitInfo
	require.NoError(t, info.Unmarshal(bz))
	info.Timestamp = time.Time{}
	bz, err = info.Marshal()
	require.NoError(t, err)
	require.NoError(t, db.Set(key, bz))

	_, err = exportFrom(db, -1, false)
	require.ErrorContains(t, err, "no commit timestamp for height 3: export from a node that executed this height")

	_, err = exportFrom(db, 2, false)
	require.NoError(t, err)
}

func TestAppExporterAllowUpgradeWindow(t *testing.T) {
	exportCmd, _, err := NewRootCmd().Find([]string{"export"})
	require.NoError(t, err)
	require.NotNil(t, exportCmd.Flags().Lookup(FlagAllowUpgradeWindow))

	db := newExportDB(t, 3, func(testApp *app.App, ctx sdk.Context) {
		require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(ctx, upgradetypes.Plan{Name: "v100", Height: 4}))
	})

	_, err = exportFrom(db, -1, false)
	require.ErrorContains(t, err, "pass --allow-upgrade-window")

	opts := viper.New()
	opts.Set(FlagAllowUpgradeWindow, true)
	exported, err := appExporter(log.NewNopLogger(), db, nil, -1, false, nil, opts, nil)
	require.NoError(t, err)
	require.Equal(t, int64(4), exported.Height)
}

// newExportDB returns a database with blocks committed up to height. lastBlock
// runs in the last block before it is committed.
func newExportDB(t *testing.T, height int64, lastBlock ...func(*app.App, sdk.Context)) dbm.DB {
	t.Helper()
	db := dbm.NewMemDB()
	testApp := app.New(log.NewNopLogger(), db, nil, 0, 0, util.EmptyAppOptions{}, baseapp.SetChainID(util.ChainID))
	genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp)
	testApp = util.InitialiseTestAppWithGenesis(testApp, app.DefaultConsensusParams(), genesisState)

	for h := int64(1); h <= height; h++ {
		_, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: h,
			Time:   util.GenesisTime.Add(time.Duration(h) * time.Second),
			Hash:   testApp.LastCommitID().Hash,
		})
		require.NoError(t, err)
		if h == height {
			for _, fn := range lastBlock {
				fn(testApp, testApp.NewUncachedContext(false, cmtproto.Header{Height: h}))
			}
		}
		_, err = testApp.Commit()
		require.NoError(t, err)
	}
	return db
}

func exportFrom(db dbm.DB, height int64, forZeroHeight bool) (servertypes.ExportedApp, error) {
	return appExporter(log.NewNopLogger(), db, nil, height, forZeroHeight, nil, util.EmptyAppOptions{}, nil)
}
