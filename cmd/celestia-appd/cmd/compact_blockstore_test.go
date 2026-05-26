package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cosmossdk.io/log"
	cometbftdb "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// seedDB creates a cometbft-db at <dir>/<name>.db, writes some entries and a
// batch of deletes, then closes it. The deletes give compaction something to
// actually do.
func seedDB(t *testing.T, name string, backend cometbftdb.BackendType, dir string) {
	t.Helper()
	db, err := cometbftdb.NewDB(name, backend, dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	for i := range 256 {
		key := []byte{byte(i)}
		require.NoError(t, db.Set(key, []byte("payload-payload-payload")))
	}
	for i := range 128 {
		require.NoError(t, db.Delete([]byte{byte(i)}))
	}
}

// newServerCtxCmd returns a cobra.Command with a server context wired up that
// points at homeDir and reports the given backend.
func newServerCtxCmd(t *testing.T, homeDir, backend string) *cobra.Command {
	t.Helper()
	cfg := cmtcfg.DefaultConfig()
	cfg.SetRoot(homeDir)
	cfg.DBBackend = backend

	sctx := server.NewDefaultContext()
	sctx.Config = cfg
	sctx.Viper = viper.New()
	sctx.Logger = log.NewNopLogger()

	cmd := compactBlockstoreCommand()
	ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
	cmd.SetContext(ctx)
	return cmd
}

func TestCompactOneCometBFTDB(t *testing.T) {
	backends := []struct {
		name    string
		backend cometbftdb.BackendType
	}{
		{"goleveldb", cometbftdb.GoLevelDBBackend},
		{"pebbledb", cometbftdb.PebbleDBBackend},
	}

	for _, b := range backends {
		t.Run(b.name+"/compacts seeded db", func(t *testing.T) {
			dir := t.TempDir()
			seedDB(t, "blockstore", b.backend, dir)

			err := compactOneCometBFTDB(log.NewNopLogger(), "blockstore", b.backend, dir)
			require.NoError(t, err)

			// DB is still usable after compaction.
			db, err := cometbftdb.NewDB("blockstore", b.backend, dir)
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			// A key that was written and not deleted is still readable.
			got, err := db.Get([]byte{byte(200)})
			require.NoError(t, err)
			require.Equal(t, []byte("payload-payload-payload"), got)

			// A key that was deleted stays deleted.
			got, err = db.Get([]byte{byte(0)})
			require.NoError(t, err)
			require.Nil(t, got)
		})
	}

	t.Run("returns wrapped error when db cannot be opened", func(t *testing.T) {
		// Pass a backend type that cometbft-db will reject.
		err := compactOneCometBFTDB(log.NewNopLogger(), "blockstore", cometbftdb.BackendType("not-a-real-backend"), t.TempDir())
		require.Error(t, err)
		require.Contains(t, err.Error(), "open")
	})
}

func TestRunCompactBlockstore_UnsupportedBackend(t *testing.T) {
	cmd := newServerCtxCmd(t, t.TempDir(), "memdb")
	require.NoError(t, cmd.Flags().Set(flagYes, "true"))

	err := runCompactBlockstore(cmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported db_backend")
	require.Contains(t, err.Error(), "memdb")
}

func TestRunCompactBlockstore_MissingDB(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "data"), 0o700))

	cmd := newServerCtxCmd(t, home, "goleveldb")
	require.NoError(t, cmd.Flags().Set(flagYes, "true"))

	err := runCompactBlockstore(cmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected database not found")
	require.Contains(t, err.Error(), "blockstore.db")
}

func TestRunCompactBlockstore_EndToEnd(t *testing.T) {
	backends := []struct {
		name    string
		backend cometbftdb.BackendType
		cfgName string
	}{
		{"goleveldb", cometbftdb.GoLevelDBBackend, "goleveldb"},
		{"pebbledb", cometbftdb.PebbleDBBackend, "pebbledb"},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			home := t.TempDir()
			dataDir := filepath.Join(home, "data")
			require.NoError(t, os.MkdirAll(dataDir, 0o700))

			seedDB(t, "blockstore", b.backend, dataDir)
			seedDB(t, "state", b.backend, dataDir)

			cmd := newServerCtxCmd(t, home, b.cfgName)
			require.NoError(t, cmd.Flags().Set(flagYes, "true"))

			require.NoError(t, runCompactBlockstore(cmd, nil))

			// Both DBs are still openable and intact after compaction.
			for _, name := range compactBlockstoreDBs {
				db, err := cometbftdb.NewDB(name, b.backend, dataDir)
				require.NoError(t, err)
				got, err := db.Get([]byte{byte(200)})
				require.NoError(t, err)
				require.Equal(t, []byte("payload-payload-payload"), got)
				require.NoError(t, db.Close())
			}
		})
	}
}
