package cmd

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
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

// newExportDB returns a database with blocks committed up to height.
func newExportDB(t *testing.T, height int64) dbm.DB {
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
		_, err = testApp.Commit()
		require.NoError(t, err)
	}
	return db
}

func exportFrom(db dbm.DB, height int64, forZeroHeight bool) (servertypes.ExportedApp, error) {
	return appExporter(log.NewNopLogger(), db, nil, height, forZeroHeight, nil, util.EmptyAppOptions{}, nil)
}
