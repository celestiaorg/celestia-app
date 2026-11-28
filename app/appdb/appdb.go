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

// WrapWALFsync makes the pebble DBs this process opens afterwards without an FS
// of their own (the app DB: cosmos-db takes no vfs.FS option) use
// walfsync.WrapFS. Their WAL fsyncs then follow the node's [storage]
// db_wal_fsync controller, and its barrier covers them. celestia-core DBs set
// their own wrapped FS on top; a second wrapper changes nothing, since a
// skipped sync of the outer file is redone through the inner one at the
// barrier or on close.
func WrapWALFsync() {
	wrapOnce.Do(func() { vfs.Default = walfsync.WrapFS(vfs.Default) })
}
