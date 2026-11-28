package app

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/internal/walfsync"
	"github.com/cockroachdb/pebble/vfs"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

// The multiplexer runs this app as a child process without CometBFT. The
// parent node skips the WAL fsyncs of its pebble databases only while it block
// syncs ([storage] db_wal_fsync in config.toml, see celestia-core
// libs/walfsync). The child applies the same setting to the app DB: it skips
// the WAL fsyncs while the blocks it executes are old, judged by the block time
// against the wall clock with the thresholds of deferred persistence. That only
// changes when writes reach the disk, never what is computed or stored.

const (
	// ConfigDBWALFsync is the config.toml key of the parent node.
	ConfigDBWALFsync = "storage.db_wal_fsync"

	walFsyncEnterLag = 10 * time.Minute
	walFsyncLeaveLag = time.Minute
)

var (
	// walFsync skips the app DB WAL fsyncs while catching up; nil keeps every
	// fsync. PrepareWALFsync sets it before the app DB opens.
	walFsync atomic.Pointer[walfsync.Controller]

	// defaultFSMu guards defaultFSWrapped.
	defaultFSMu      sync.Mutex
	defaultFSWrapped bool
)

// PrepareWALFsync runs in the start command before the app DB opens. It
// refuses to start after an OS crash that happened while app DB WAL fsyncs were
// skipped, unless accept is set. When CometBFT is not in this process and the
// app DB is pebble, it makes the app DB follow db_wal_fsync.
func PrepareWALFsync(appOpts servertypes.AppOptions, home string, withComet, accept bool, logger log.Logger) error {
	mode := cast.ToString(appOpts.Get(ConfigDBWALFsync))
	if err := walfsync.ValidateMode(mode); err != nil {
		return err
	}
	dataDir := filepath.Join(home, "data")
	found, err := walfsync.CheckMarker([]string{dataDir}, accept)
	if err != nil {
		return err
	}
	switch {
	case found && accept:
		logger.Error("--" + walfsync.AcceptFlag + " is set: starting even if the OS crashed while app DB WAL fsyncs were skipped. " +
			"If the handshake fails, roll back or re-sync from a snapshot.")
	case found:
		logger.Info("Previous run stopped while app DB WAL fsyncs were skipped; the OS did not crash, so the WAL files were fsynced")
	}
	if withComet || mode == walfsync.ModeAlways || server.GetAppDBBackend(appOpts) != dbm.PebbleDBBackend {
		return nil
	}
	// The app DB is the only pebble DB of a child process, and cosmos-db takes
	// no vfs.FS option, so wrap the default FS that pebble.Open falls back to.
	defaultFSMu.Lock()
	if !defaultFSWrapped {
		vfs.Default = walfsync.WrapFS(vfs.Default)
		defaultFSWrapped = true
	}
	defaultFSMu.Unlock()
	walFsync.Store(walfsync.NewController(mode, dataDir))
	return nil
}

// walFsyncCatchUp starts skipping the app DB WAL fsyncs at the first block
// older than walFsyncEnterLag, and fsyncs every WAL and stops skipping at the
// first block younger than walFsyncLeaveLag. It reads the wall clock only to
// decide when writes reach the disk.
func (app *App) walFsyncCatchUp(blockTime time.Time) {
	c := walFsync.Load()
	if c == nil || blockTime.IsZero() {
		return
	}
	switch lag := time.Since(blockTime); {
	case lag > walFsyncEnterLag:
		if err := c.StartCatchup(); err != nil {
			app.Logger().Error("Keeping app DB WAL fsyncs on", "err", err)
		}
	case lag < walFsyncLeaveLag:
		if err := c.FinishCatchup(); err != nil {
			app.Logger().Error("Failed to fsync the app DB WAL files", "err", err)
		}
	}
}

// Close closes the app and then makes every app DB write durable.
func (app *App) Close() error {
	return errors.Join(app.BaseApp.Close(), walFsync.Load().FinishCatchup())
}
