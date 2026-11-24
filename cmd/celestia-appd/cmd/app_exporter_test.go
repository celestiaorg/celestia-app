package cmd

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

type exporterAppOptions map[string]any

func (o exporterAppOptions) Get(key string) any { return o[key] }

// writeCountingDB counts every write made through it, including batches.
type writeCountingDB struct {
	dbm.DB
	writes *int
}

func (d writeCountingDB) Set(k, v []byte) error     { *d.writes++; return d.DB.Set(k, v) }
func (d writeCountingDB) SetSync(k, v []byte) error { *d.writes++; return d.DB.SetSync(k, v) }
func (d writeCountingDB) Delete(k []byte) error     { *d.writes++; return d.DB.Delete(k) }
func (d writeCountingDB) DeleteSync(k []byte) error { *d.writes++; return d.DB.DeleteSync(k) }
func (d writeCountingDB) NewBatch() dbm.Batch {
	return writeCountingBatch{d.DB.NewBatch(), d.writes}
}

type writeCountingBatch struct {
	dbm.Batch
	writes *int
}

func (b writeCountingBatch) Set(k, v []byte) error { *b.writes++; return b.Batch.Set(k, v) }
func (b writeCountingBatch) Delete(k []byte) error { *b.writes++; return b.Batch.Delete(k) }

// TestAppExporterDoesNotWriteDB checks that export, which sets
// server.KeyReadOnlyLoad, does not write to application.db. The state is
// committed with IAVL fast nodes disabled, as a node running deferred
// persistence in "sync" mode does, so a load with fast nodes enabled would
// rebuild the fast-node index and write it to the DB.
func TestAppExporterDoesNotWriteDB(t *testing.T) {
	db := dbm.NewMemDB()
	cparams := app.DefaultConsensusParams()
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp := app.New(log.NewNopLogger(), db, nil, 0, encCfg, 0, 0, exporterAppOptions{}, baseapp.SetIAVLDisableFastNode(true))
	genesisState, _, _ := testutil.GenesisStateWithSingleValidator(testApp)
	testutil.InitialiseTestAppWithGenesis(testApp, cparams, genesisState)
	testApp.Commit()
	require.Positive(t, testApp.LastBlockHeight())

	writes := 0
	opts := exporterAppOptions{server.KeyReadOnlyLoad: true}
	exported, err := appExporter(log.NewNopLogger(), writeCountingDB{db, &writes}, nil, -1, false, nil, opts)
	require.NoError(t, err)
	require.NotEmpty(t, exported.AppState)
	require.Zero(t, writes, "export wrote to the DB")
}
