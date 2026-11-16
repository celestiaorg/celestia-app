// Package appdb opens the application database.
package appdb

import (
	"path/filepath"

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
