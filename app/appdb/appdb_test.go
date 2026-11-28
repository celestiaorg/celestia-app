package appdb_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/celestiaorg/celestia-app/v12/app/appdb"
	"github.com/cockroachdb/pebble/vfs"
	"github.com/cometbft/cometbft/libs/walfsync"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"
)

func TestOpenPebbleCacheSize(t *testing.T) {
	root := t.TempDir()
	db, err := appdb.Open(root, dbm.PebbleDBBackend)
	require.NoError(t, err)
	defer db.Close()
	require.IsType(t, &dbm.PebbleDB{}, db)

	// pebble records its options, including the block cache size, on open.
	files, err := filepath.Glob(filepath.Join(root, "data", "application.db", "OPTIONS-*"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	bz, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`(?m)^\s*cache_size=134217728$`), string(bz))
}

func TestOpenGoLevelDB(t *testing.T) {
	root := t.TempDir()
	db, err := appdb.Open(root, dbm.GoLevelDBBackend)
	require.NoError(t, err)
	defer db.Close()
	require.IsType(t, &dbm.GoLevelDB{}, db)
	require.DirExists(t, filepath.Join(root, "data", "application.db"))
}

// TestWrapWALFsync checks that the app DB opened after WrapWALFsync follows a
// walfsync controller.
func TestWrapWALFsync(t *testing.T) {
	orig := vfs.Default
	appdb.WrapWALFsync()
	require.NotEqual(t, orig, vfs.Default, "default FS wrapped")
	wrapped := vfs.Default
	appdb.WrapWALFsync()
	require.Equal(t, wrapped, vfs.Default, "wrapped once")

	root := t.TempDir()
	db, err := appdb.Open(root, dbm.PebbleDBBackend)
	require.NoError(t, err)
	c := walfsync.NewController(walfsync.ModeCatchup, filepath.Join(root, "data"))
	require.NoError(t, c.StartCatchup())
	require.NoError(t, db.SetSync([]byte("k"), []byte("v")))
	require.NoError(t, c.FinishCatchup())
	require.NoFileExists(t, filepath.Join(root, "data", walfsync.MarkerFile))
	require.NoError(t, db.Close())
}
