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
			require.NoError(t, ldb.Set(fmt.Appendf(nil, "key-%s-%08d", name, i), val))
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
	return migrateOpts{homeDir: home, backup: true, batchSizeMB: 1, deleteChunkMB: defaultDeleteChunkMB, parallel: 3, manualSwap: true}
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

// TestCopyAndDeleteKeys_NoKeyLoss verifies that no keys are dropped when the
// incremental delete threshold is reached and the source iterator is reopened.
// Uses a small deleteChunkBytes (10 KB) to trigger the reopen path multiple times.
func TestCopyAndDeleteKeys_NoKeyLoss(t *testing.T) {
	dir := t.TempDir()
	numKeys := 200
	srcDB, err := db.NewDB("src", db.GoLevelDBBackend, dir)
	require.NoError(t, err)
	expected := make(map[string][]byte, numKeys)
	for i := range numKeys {
		key := fmt.Appendf(nil, "key-%06d", i)
		val := make([]byte, 256)
		_, _ = rand.Read(val)
		require.NoError(t, srcDB.Set(key, val))
		expected[string(key)] = val
	}

	srcIter, err := srcDB.Iterator(nil, nil)
	require.NoError(t, err)
	destDB, err := db.NewDB("dst", db.PebbleDBBackend, dir)
	require.NoError(t, err)

	// 10 KB threshold triggers delete-and-reopen ~5 times for 200×256B keys
	smallChunk := int64(10 * 1024)
	totalKeys, _, err := copyAndDeleteKeys(context.Background(), "test", srcDB, destDB, srcIter, 1024*1024, 0, smallChunk, time.Now())
	require.NoError(t, err)
	assert.Equal(t, int64(numKeys), totalKeys, "reported key count wrong")
	assert.Equal(t, int64(numKeys), countKeys(t, destDB), "dest key count wrong — keys were lost")

	for k, v := range expected {
		dv, err := destDB.Get([]byte(k))
		require.NoError(t, err)
		assert.True(t, bytes.Equal(v, dv), "value mismatch at %s", k)
	}

	_ = srcDB.Close()
	_ = destDB.Close()
}

func TestIsPebbleDB(t *testing.T) {
	dir := t.TempDir()

	// Create a LevelDB database
	levelDir := filepath.Join(dir, "level")
	require.NoError(t, os.MkdirAll(levelDir, 0o755))
	ldb, err := db.NewDB("test", db.GoLevelDBBackend, levelDir)
	require.NoError(t, err)
	require.NoError(t, ldb.Set([]byte("k"), []byte("v")))
	require.NoError(t, ldb.Close())
	assert.False(t, isPebbleDB(filepath.Join(levelDir, "test.db")), "LevelDB should not be detected as PebbleDB")

	// Create a PebbleDB database
	pebbleDir := filepath.Join(dir, "pebble")
	require.NoError(t, os.MkdirAll(pebbleDir, 0o755))
	pdb, err := db.NewDB("test", db.PebbleDBBackend, pebbleDir)
	require.NoError(t, err)
	require.NoError(t, pdb.Set([]byte("k"), []byte("v")))
	require.NoError(t, pdb.Close())
	assert.True(t, isPebbleDB(filepath.Join(pebbleDir, "test.db")), "PebbleDB should be detected as PebbleDB")

	// Non-existent path
	assert.False(t, isPebbleDB(filepath.Join(dir, "nonexistent.db")), "non-existent path should return false")
}

func TestMigration_SkipsWhenConfigIsPebbleDB(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"data", "config"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte("db_backend = \"pebbledb\"\n"), 0o644))

	// Create a LevelDB database — it should never be touched because config says pebbledb
	ldb, err := db.NewDB("application", db.GoLevelDBBackend, filepath.Join(home, "data"))
	require.NoError(t, err)
	require.NoError(t, ldb.Set([]byte("k"), []byte("v")))
	require.NoError(t, ldb.Close())

	o := migrateOpts{homeDir: home, backup: true, batchSizeMB: 1, deleteChunkMB: defaultDeleteChunkMB, parallel: 3, manualSwap: true}
	require.NoError(t, runMigration(context.Background(), o))

	// Verify no data_pebble directory was created (early return before any work)
	_, err = os.Stat(filepath.Join(home, "data_pebble"))
	assert.True(t, os.IsNotExist(err), "data_pebble should not exist — migration should have returned early")
}

func TestMigration_ErrorsOnPebbleDBSource(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"data", "config"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	// Config says goleveldb, but the files on disk are already PebbleDB
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte("db_backend = \"goleveldb\"\n"), 0o644))

	for _, name := range allDatabases {
		pdb, err := db.NewDB(name, db.PebbleDBBackend, filepath.Join(home, "data"))
		require.NoError(t, err)
		require.NoError(t, pdb.Set([]byte("k"), []byte("v")))
		require.NoError(t, pdb.Close())
	}

	o := migrateOpts{homeDir: home, backup: true, batchSizeMB: 1, deleteChunkMB: defaultDeleteChunkMB, parallel: 3, manualSwap: false}
	err := runMigration(context.Background(), o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already PebbleDB")
}

func TestDeleteSourceKeys(t *testing.T) {
	dir := t.TempDir()
	ldb, err := db.NewDB("t", db.GoLevelDBBackend, dir)
	require.NoError(t, err)
	defer func() { _ = ldb.Close() }()
	keys := make([][]byte, 100)
	for i := range keys {
		keys[i] = fmt.Appendf(nil, "k%04d", i)
		require.NoError(t, ldb.Set(keys[i], []byte("v")))
	}
	require.NoError(t, deleteSourceKeys(ldb, keys))
	assert.Equal(t, int64(0), countKeys(t, ldb))
	require.NoError(t, deleteSourceKeys(ldb, nil))
}
