package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v4/internal/walfsync"
	"github.com/cockroachdb/pebble/vfs"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/stretchr/testify/require"
)

type walFsyncOpts map[string]any

func (o walFsyncOpts) Get(key string) any { return o[key] }

// resetWALFsync restores the process-wide state PrepareWALFsync changes.
func resetWALFsync(t *testing.T) {
	orig := vfs.Default
	t.Cleanup(func() {
		require.NoError(t, walFsync.Load().FinishCatchup())
		walFsync.Store(nil)
		defaultFSMu.Lock()
		vfs.Default, defaultFSWrapped = orig, false
		defaultFSMu.Unlock()
	})
}

func TestPrepareWALFsyncWiring(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mode      string
		backend   string
		withComet bool
		wired     bool
	}{
		{name: "child, default mode", backend: "pebbledb", wired: true},
		{name: "child, catchup", mode: "catchup", backend: "pebbledb", wired: true},
		{name: "child, always", mode: "always", backend: "pebbledb"},
		{name: "child, goleveldb", backend: "goleveldb"},
		{name: "with comet", backend: "pebbledb", withComet: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetWALFsync(t)
			orig := vfs.Default
			opts := walFsyncOpts{ConfigDBWALFsync: tc.mode, "app-db-backend": tc.backend}
			require.NoError(t, PrepareWALFsync(opts, t.TempDir(), tc.withComet, false, log.NewNopLogger()))
			require.Equal(t, tc.wired, walFsync.Load() != nil)
			require.Equal(t, tc.wired, vfs.Default != orig, "default FS wrapped")
		})
	}
}

func TestPrepareWALFsyncInvalidMode(t *testing.T) {
	resetWALFsync(t)
	opts := walFsyncOpts{ConfigDBWALFsync: "never", "app-db-backend": "pebbledb"}
	require.ErrorContains(t, PrepareWALFsync(opts, t.TempDir(), false, false, log.NewNopLogger()), "db_wal_fsync")
}

func TestPrepareWALFsyncMarker(t *testing.T) {
	for _, accept := range []bool{false, true} {
		resetWALFsync(t)
		home := t.TempDir()
		marker := filepath.Join(home, "data", walfsync.MarkerFile)
		require.NoError(t, os.MkdirAll(filepath.Dir(marker), 0o700))
		require.NoError(t, os.WriteFile(marker, []byte("another-boot\n"), 0o600))

		err := PrepareWALFsync(walFsyncOpts{"app-db-backend": "pebbledb"}, home, false, accept, log.NewNopLogger())
		if accept {
			require.NoError(t, err)
			require.NoFileExists(t, marker)
		} else {
			require.ErrorIs(t, err, walfsync.ErrUnsyncedCrash)
			require.ErrorContains(t, err, walfsync.AcceptFlag)
			require.FileExists(t, marker)
		}
	}
}

// TestWALFsyncCatchUp checks that the block time drives the controller with a
// single threshold and that Close removes the marker.
func TestWALFsyncCatchUp(t *testing.T) {
	resetWALFsync(t)
	home := t.TempDir()
	require.NoError(t, PrepareWALFsync(walFsyncOpts{"app-db-backend": "pebbledb"}, home, false, false, log.NewNopLogger()))
	marker := filepath.Join(home, "data", walfsync.MarkerFile)

	db, err := dbm.NewDB("application", dbm.PebbleDBBackend, filepath.Join(home, "data"))
	require.NoError(t, err)
	app := &App{BaseApp: baseapp.NewBaseApp("test", log.NewNopLogger(), db, nil)}

	app.walFsyncCatchUp(time.Now().Add(-5 * time.Minute))
	require.NoFileExists(t, marker, "a block younger than 10 minutes does not start a catch-up")
	app.walFsyncCatchUp(time.Now().Add(-20 * time.Minute))
	require.FileExists(t, marker, "an old block starts the catch-up")
	require.NoError(t, db.SetSync([]byte("k"), []byte("v")))
	app.walFsyncCatchUp(time.Now().Add(-5 * time.Minute))
	require.NoFileExists(t, marker, "a block 5 minutes old finishes the catch-up (clock skew)")

	app.walFsyncCatchUp(time.Now().Add(-time.Hour))
	require.FileExists(t, marker)
	require.NoError(t, app.Close())
	require.NoFileExists(t, marker, "Close finishes the catch-up")
}
