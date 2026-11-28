package app_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/internal/walfsync"
	testutil "github.com/celestiaorg/celestia-app/v8/test/util"
	"github.com/celestiaorg/celestia-app/v8/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/stretchr/testify/require"
)

func newWALFsyncTestApp(t *testing.T, home string) *app.App {
	db, err := dbm.NewDB("application", dbm.PebbleDBBackend, filepath.Join(home, "data"))
	require.NoError(t, err)
	return app.New(testutil.TestAppLogger, db, nil, 0, interBlockCacheAppOptions{}, baseapp.SetChainID(testutil.ChainID))
}

// TestWALFsyncDeterminism runs the same old blocks on a pebble app DB with
// every WAL fsync and on one that skips them while catching up, and checks
// that every result and app hash is identical. PrepareWALFsync wraps pebble's
// default FS for the rest of this test binary; with no catch-up running that
// changes nothing.
func TestWALFsyncDeterminism(t *testing.T) {
	const blocks = 10
	accounts := testfactory.GenerateAccounts(4)

	stockHome, skipHome := t.TempDir(), t.TempDir()
	stock := newWALFsyncTestApp(t, stockHome) // opened before the FS is wrapped
	opts := interBlockCacheAppOptions{app.ConfigDBWALFsync: walfsync.ModeCatchup, "app-db-backend": "pebbledb"}
	require.NoError(t, app.PrepareWALFsync(opts, skipHome, false, false, log.NewNopLogger()))
	skip := newWALFsyncTestApp(t, skipHome)
	marker := filepath.Join(skipHome, "data", walfsync.MarkerFile)

	genesis, valSet, kr := testutil.GenesisStateWithSingleValidator(stock, accounts...)
	testutil.InitialiseTestAppWithGenesis(stock, app.DefaultConsensusParams(), genesis)
	testutil.InitialiseTestAppWithGenesis(skip, app.DefaultConsensusParams(), genesis)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	for i := 1; i <= blocks; i++ {
		var txs [][]byte
		if i > 1 {
			for j, acc := range accounts {
				for _, tx := range testutil.SendTxsWithAccounts(t, stock, enc.TxConfig, kr, uint64(1000+i), accounts[(j+1)%len(accounts)], []string{acc}, testutil.ChainID) {
					txs = append(txs, tx)
				}
			}
		}
		req := &abci.RequestFinalizeBlock{
			Time:               testutil.GenesisTime.Add(time.Duration(i) * 6 * time.Second),
			Height:             stock.LastBlockHeight() + 1,
			Hash:               stock.LastCommitID().Hash,
			NextValidatorsHash: valSet.Hash(),
			ProposerAddress:    valSet.Validators[0].Address,
			Txs:                txs,
		}
		stockRes, err := stock.FinalizeBlock(req)
		require.NoError(t, err)
		skipRes, err := skip.FinalizeBlock(req)
		require.NoError(t, err)
		for _, r := range stockRes.TxResults {
			require.EqualValues(t, 0, r.Code, r.Log)
		}
		require.Equal(t, stockRes, skipRes, "block %d", i)
		_, err = stock.Commit()
		require.NoError(t, err)
		_, err = skip.Commit()
		require.NoError(t, err)
		require.Equal(t, stock.LastCommitID(), skip.LastCommitID(), "block %d", i)
		require.FileExists(t, marker, "old blocks skip the WAL fsyncs")
	}

	require.NoError(t, stock.Close())
	require.NoError(t, skip.Close())
	_, err := os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist, "Close fsyncs the WAL and removes the marker")
}
