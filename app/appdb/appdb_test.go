package appdb_test

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v12/app/appdb"
	"github.com/cockroachdb/pebble/vfs"
	cmtdb "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
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

// syncCountingFS counts the fsyncs of each WAL file. It stands for the disk
// under every walfsync layer.
type syncCountingFS struct {
	vfs.FS
	mu    sync.Mutex
	syncs map[string]int
}

func (fs *syncCountingFS) Create(name string) (vfs.File, error) {
	f, err := fs.FS.Create(name)
	return fs.track(name, f, err)
}

func (fs *syncCountingFS) ReuseForWrite(oldname, newname string) (vfs.File, error) {
	f, err := fs.FS.ReuseForWrite(oldname, newname)
	return fs.track(newname, f, err)
}

func (fs *syncCountingFS) track(name string, f vfs.File, err error) (vfs.File, error) {
	if err != nil || !walfsync.IsWAL(name) {
		return f, err
	}
	fs.mu.Lock()
	fs.syncs[name] += 0
	fs.mu.Unlock()
	return &syncCountingFile{File: f, fs: fs, name: name}, nil
}

func (fs *syncCountingFS) counts() map[string]int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return maps.Clone(fs.syncs)
}

type syncCountingFile struct {
	vfs.File
	fs   *syncCountingFS
	name string
}

func (f *syncCountingFile) Sync() error {
	f.fs.mu.Lock()
	f.fs.syncs[f.name]++
	f.fs.mu.Unlock()
	return f.File.Sync()
}

func (f *syncCountingFile) SyncData() error { return f.Sync() }

// TestWrapWALFsync opens a celestia-core DB and the app DB in one process
// after WrapWALFsync, with catch-up on. The core DB gets no second wrapper,
// no WAL fsync reaches the disk while catching up, and the barrier fsyncs
// every WAL of both DBs exactly once.
func TestWrapWALFsync(t *testing.T) {
	disk := &syncCountingFS{FS: vfs.Default, syncs: map[string]int{}}
	vfs.Default = disk // before the first wrap; WrapWALFsync runs once per process
	appdb.WrapWALFsync()
	wrapped := vfs.Default
	require.NotEqual(t, vfs.FS(disk), wrapped, "default FS wrapped")
	appdb.WrapWALFsync()
	require.Equal(t, wrapped, vfs.Default, "wrapped once")
	require.Equal(t, wrapped, walfsync.WrapFS(vfs.Default), "core's WrapFS adds no second layer")

	root := t.TempDir()
	cfg := cmtcfg.DefaultConfig()
	cfg.SetRoot(root)
	cfg.DBBackend = string(cmtdb.PebbleDBBackend)
	cfg.Storage.DBWALFsync = walfsync.ModeCatchup
	stateDB, err := cmtcfg.DefaultDBProvider(&cmtcfg.DBContext{ID: "state", Config: cfg})
	require.NoError(t, err)
	appDB, err := appdb.Open(root, dbm.PebbleDBBackend)
	require.NoError(t, err)

	c := walfsync.NewController(walfsync.ModeCatchup, cfg.DBDir())
	require.NoError(t, c.StartCatchup())
	require.NoError(t, stateDB.SetSync([]byte("k"), []byte("v")))
	require.NoError(t, appDB.SetSync([]byte("k"), []byte("v")))
	before := disk.counts()
	require.Len(t, before, 2, "one WAL per DB")
	for name, n := range before {
		require.Zero(t, n, "%s fsynced while catching up", name)
	}

	require.NoError(t, c.FinishCatchup())
	for name, n := range disk.counts() {
		require.Equal(t, 1, n, "barrier fsyncs %s once", name)
	}
	require.NoFileExists(t, filepath.Join(cfg.DBDir(), walfsync.MarkerFile))
	require.NoError(t, stateDB.Close())
	require.NoError(t, appDB.Close())
}
