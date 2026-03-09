package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	db "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestNode(t *testing.T, keysPerDB, valueSize int) string {
	t.Helper()
	home := t.TempDir()
	for _, d := range []string{"data", "config"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte("db_backend = \"goleveldb\"\n"), 0o644))
	for _, name := range allDatabases {
		ldb, err := db.NewDB(name, db.GoLevelDBBackend, filepath.Join(home, "data"))
		require.NoError(t, err)
		for i := range keysPerDB {
			val := make([]byte, valueSize)
			_, _ = rand.Read(val)
			require.NoError(t, ldb.Set([]byte(fmt.Sprintf("key-%s-%08d", name, i)), val))
		}
		require.NoError(t, ldb.Close())
	}
	return home
}

func countKeys(t *testing.T, d db.DB) int64 {
	t.Helper()
	iter, err := d.Iterator(nil, nil)
	require.NoError(t, err)
	var n int64
	for ; iter.Valid(); iter.Next() {
		n++
	}
	require.NoError(t, iter.Close())
	return n
}

func opts(home string) migrateOpts {
	return migrateOpts{homeDir: home, backup: true, batchSizeMB: 1, parallel: 3, manualSwap: true}
}

func TestMigration_BackupAndVerify(t *testing.T) {
	home := setupTestNode(t, 5000, 1024)
	require.NoError(t, runMigration(context.Background(), opts(home)))
	for _, name := range allDatabases {
		src, err := db.NewDB(name, db.GoLevelDBBackend, filepath.Join(home, "data"))
		require.NoError(t, err)
		dst, err := db.NewDB(name, db.PebbleDBBackend, filepath.Join(home, "data_pebble"))
		require.NoError(t, err)
		assert.Equal(t, countKeys(t, src), countKeys(t, dst), "[%s] key count mismatch", name)
		iter, err := src.Iterator(nil, nil)
		require.NoError(t, err)
		for ; iter.Valid(); iter.Next() {
			dv, err := dst.Get(iter.Key())
			require.NoError(t, err)
			assert.True(t, bytes.Equal(iter.Value(), dv), "[%s] value mismatch at %x", name, iter.Key())
		}
		_ = iter.Close()
		_ = src.Close()
		_ = dst.Close()
	}
}

func TestMigration_NoBackupDeletesSource(t *testing.T) {
	home := setupTestNode(t, 200, 512)
	o := opts(home)
	o.backup = false
	require.NoError(t, runMigration(context.Background(), o))
	for _, name := range allDatabases {
		_, err := os.Stat(filepath.Join(home, "data", name+".db"))
		assert.True(t, os.IsNotExist(err), "[%s] source not deleted", name)
	}
}

func TestMigration_ResumeAfterInterrupt(t *testing.T) {
	home := setupTestNode(t, 5000, 1024)
	o := opts(home)
	o.parallel = 1
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = runMigration(ctx, o)

	require.NoError(t, runMigration(context.Background(), o))
	for _, name := range allDatabases {
		dst, err := db.NewDB(name, db.PebbleDBBackend, filepath.Join(home, "data_pebble"))
		require.NoError(t, err)
		assert.Equal(t, int64(5000), countKeys(t, dst), "[%s]", name)
		_ = dst.Close()
	}
}

func TestMigration_AutoSwap(t *testing.T) {
	home := setupTestNode(t, 100, 256)
	o := opts(home)
	o.manualSwap = false
	require.NoError(t, runMigration(context.Background(), o))
	for _, name := range allDatabases {
		dst, err := db.NewDB(name, db.PebbleDBBackend, filepath.Join(home, "data"))
		require.NoError(t, err)
		assert.Equal(t, int64(100), countKeys(t, dst), "[%s]", name)
		_ = dst.Close()
	}
	cfg, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(cfg), `db_backend = "pebbledb"`)
}

func TestMigration_AutoSwapBlockedByFilter(t *testing.T) {
	home := setupTestNode(t, 50, 128)
	o := opts(home)
	o.dbFilter = "blockstore"
	o.manualSwap = false
	require.NoError(t, runMigration(context.Background(), o))
	cfg, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	require.NoError(t, err)
	assert.NotContains(t, string(cfg), "pebbledb")
}

func TestMigration_BackupMismatchOnResume(t *testing.T) {
	home := setupTestNode(t, 50, 128)
	require.NoError(t, runMigration(context.Background(), opts(home)))
	o := opts(home)
	o.backup = false
	err := runMigration(context.Background(), o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--backup")
}

func TestSaveAndLoadState(t *testing.T) {
	dir := t.TempDir()
	state := &MigrationState{Backup: true, Databases: map[string]DBState{
		"app": {Status: statusMigrated, KeysMigrated: 42},
		"blk": {Status: statusPending},
	}}
	require.NoError(t, saveState(state, dir))
	loaded, err := loadState(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, int64(42), loaded.Databases["app"].KeysMigrated)
	assert.Equal(t, statusPending, loaded.Databases["blk"].Status)

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".migration_state.json"), []byte("{bad"), 0o644))
	_, err = loadState(dir)
	require.Error(t, err)
}

func TestFindResumePoint(t *testing.T) {
	dir := t.TempDir()
	pdb, err := db.NewDB("t", db.PebbleDBBackend, dir)
	require.NoError(t, err)
	defer func() { _ = pdb.Close() }()

	key, err := findResumePoint(pdb, "t")
	require.NoError(t, err)
	assert.Nil(t, key)

	for _, k := range []string{"aaa", "mmm", "zzz"} {
		require.NoError(t, pdb.Set([]byte(k), []byte("v")))
	}
	key, err = findResumePoint(pdb, "t")
	require.NoError(t, err)
	assert.Equal(t, []byte("zzz"), key)
}

func TestIteratorFrom(t *testing.T) {
	dir := t.TempDir()
	ldb, err := db.NewDB("t", db.GoLevelDBBackend, dir)
	require.NoError(t, err)
	defer func() { _ = ldb.Close() }()
	for _, k := range []string{"a", "b", "c", "d"} {
		require.NoError(t, ldb.Set([]byte(k), []byte("v")))
	}

	iter, err := iteratorFrom(ldb, nil)
	require.NoError(t, err)
	assert.Equal(t, "a", string(iter.Key()))
	_ = iter.Close()

	iter, err = iteratorFrom(ldb, []byte("b"))
	require.NoError(t, err)
	assert.Equal(t, "c", string(iter.Key()))
	_ = iter.Close()
}

func TestDeleteSourceKeys(t *testing.T) {
	dir := t.TempDir()
	ldb, err := db.NewDB("t", db.GoLevelDBBackend, dir)
	require.NoError(t, err)
	defer func() { _ = ldb.Close() }()
	keys := make([][]byte, 100)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("k%04d", i))
		require.NoError(t, ldb.Set(keys[i], []byte("v")))
	}
	require.NoError(t, deleteSourceKeys(ldb, keys))
	assert.Equal(t, int64(0), countKeys(t, ldb))
	require.NoError(t, deleteSourceKeys(ldb, nil))
}
