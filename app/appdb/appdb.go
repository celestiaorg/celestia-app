// Package appdb opens the application database.
package appdb

import (
	"path/filepath"
	"sync"

	"github.com/cockroachdb/pebble/vfs"
	"github.com/cometbft/cometbft/libs/walfsync"
	dbm "github.com/cosmos/cosmos-db"
)

// PebbleCacheSize is the block cache of a pebble application DB. Pebble's 8 MiB
// default is reserved by the two 4 MiB memtables, so it rarely serves a hit.
const PebbleCacheSize = 128 << 20

// Open opens rootDir/data/application.db, giving a pebble DB a PebbleCacheSize
// block cache. Other backends open with their defaults.
func Open(rootDir string, backend dbm.BackendType) (dbm.DB, error) {
	var opts dbm.Options
	if backend == dbm.PebbleDBBackend {
		opts = dbm.OptionsMap{dbm.PebbleCacheSizeKey: PebbleCacheSize}
	}
	return dbm.NewDBwithOptions("application", backend, filepath.Join(rootDir, "data"), opts)
}

var wrapOnce sync.Once

// WrapWALFsync makes every pebble DB this process opens afterwards without an
// FS of its own use walfsync.WrapFS, so that WAL fsyncs follow the node's
// [storage] db_wal_fsync controller and its barrier covers them. It replaces
// pebble's default FS because cosmos-db takes no vfs.FS option, so the app DB
// cannot be given one. The start command calls it once, before any pebble DB
// opens. celestia-core DBs wrap the default FS again, and WrapFS returns an
// already wrapped FS unchanged, so every WAL file has exactly one wrapper.
func WrapWALFsync() {
	wrapOnce.Do(func() { vfs.Default = walfsync.WrapFS(vfs.Default) })
}
