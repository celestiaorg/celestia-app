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

// testMainDBs are the databases created under <home>/data by setupTestNode.
// (snapshots/metadata is created separately by tests that need it.)
var testMainDBs = []string{"application", "blockstore", "state", "tx_index", "evidence"}

func setupTestNode(t *testing.T, keysPerDB, valueSize int) string {
	t.Helper()
	home := t.TempDir()
	for _, d := range []string{"data", "config"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte("db_backend = \"goleveldb\"\n"), 0o644))
	for _, name := range testMainDBs {
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

// stagedDB opens the in-progress (staged) PebbleDB for a database under <home>/data.
func stagedDB(t *testing.T, home, fileName string) db.DB {
	t.Helper()
	d, err := db.NewDB(fileName, db.PebbleDBBackend, filepath.Join(home, "data", stagingDirName))
	require.NoError(t, err)
	return d
}

// finalDB opens the swapped-into-place PebbleDB under <home>/data.
func finalDB(t *testing.T, home, fileName string) db.DB {
	t.Helper()
	d, err := db.NewDB(fileName, db.PebbleDBBackend, filepath.Join(home, "data"))
	require.NoError(t, err)
	return d
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
	for _, name := range testMainDBs {
		src, err := db.NewDB(name, db.GoLevelDBBackend, filepath.Join(home, "data"))
		require.NoError(t, err)
		dst := stagedDB(t, home, name)
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
	for _, name := range testMainDBs {
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
	for _, name := range testMainDBs {
		dst := stagedDB(t, home, name)
		assert.Equal(t, int64(5000), countKeys(t, dst), "[%s]", name)
		_ = dst.Close()
	}
}

func TestMigration_AutoSwap(t *testing.T) {
	home := setupTestNode(t, 100, 256)
	o := opts(home)
	o.manualSwap = false
	o.backup = false
	require.NoError(t, runMigration(context.Background(), o))
	for _, name := range testMainDBs {
		assert.True(t, isPebbleDB(filepath.Join(home, "data", name+".db")), "[%s] not swapped to pebble", name)
		dst := finalDB(t, home, name)
		assert.Equal(t, int64(100), countKeys(t, dst), "[%s]", name)
		_ = dst.Close()
	}
	cfg, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(cfg), `db_backend = "pebbledb"`)
	// staging + state dir cleaned up
	_, err = os.Stat(filepath.Join(home, "data", stagingDirName))
	assert.True(t, os.IsNotExist(err), "staging dir should be removed")
}

func TestMigration_AutoSwapPreservesBackup(t *testing.T) {
	home := setupTestNode(t, 100, 256)
	o := opts(home)
	o.manualSwap = false
	o.backup = true
	require.NoError(t, runMigration(context.Background(), o))
	for _, name := range testMainDBs {
		// New DB is pebble in place.
		assert.True(t, isPebbleDB(filepath.Join(home, "data", name+".db")), "[%s] not swapped", name)
		// Old LevelDB preserved as a backup directory.
		_, err := os.Stat(filepath.Join(home, "data", name+".db"+backupSuffix))
		assert.NoError(t, err, "[%s] backup should be preserved", name)
	}
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
	assert.Contains(t, err.Error(), "backup")
}

func TestMigration_SnapshotsMetadata(t *testing.T) {
	home := setupTestNode(t, 50, 128)
	// Create data/snapshots/metadata.db as a LevelDB.
	snapDir := filepath.Join(home, "data", "snapshots")
	require.NoError(t, os.MkdirAll(snapDir, 0o755))
	mdb, err := db.NewDB("metadata", db.GoLevelDBBackend, snapDir)
	require.NoError(t, err)
	require.NoError(t, mdb.Set([]byte("snap-key"), []byte("snap-val")))
	require.NoError(t, mdb.Close())

	o := opts(home)
	o.manualSwap = false
	o.backup = false
	require.NoError(t, runMigration(context.Background(), o))

	assert.True(t, isPebbleDB(filepath.Join(snapDir, "metadata.db")), "snapshots/metadata not migrated")
	d, err := db.NewDB("metadata", db.PebbleDBBackend, snapDir)
	require.NoError(t, err)
	v, err := d.Get([]byte("snap-key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("snap-val"), v)
	_ = d.Close()
}

func TestMigration_CustomDBDir(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"data", "config"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	customDir := filepath.Join(home, "customdb")
	require.NoError(t, os.MkdirAll(customDir, 0o755))
	// config.toml points db_dir at customDir (absolute path).
	cfg := fmt.Sprintf("db_backend = \"goleveldb\"\ndb_dir = \"%s\"\n", customDir)
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte(cfg), 0o644))

	// application lives under data/, consensus DBs under customDir.
	adb, err := db.NewDB("application", db.GoLevelDBBackend, filepath.Join(home, "data"))
	require.NoError(t, err)
	require.NoError(t, adb.Set([]byte("a"), []byte("1")))
	require.NoError(t, adb.Close())
	for _, name := range []string{"blockstore", "state", "evidence"} {
		cdb, err := db.NewDB(name, db.GoLevelDBBackend, customDir)
		require.NoError(t, err)
		require.NoError(t, cdb.Set([]byte("k"), []byte("v")))
		require.NoError(t, cdb.Close())
	}

	o := opts(home)
	o.manualSwap = false
	o.backup = false
	require.NoError(t, runMigration(context.Background(), o))

	assert.True(t, isPebbleDB(filepath.Join(home, "data", "application.db")), "application not migrated")
	for _, name := range []string{"blockstore", "state", "evidence"} {
		assert.True(t, isPebbleDB(filepath.Join(customDir, name+".db")), "[%s] consensus DB in custom db_dir not migrated", name)
	}
}

func TestEffectiveBackend_AppTomlPrecedence(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte("db_backend = \"goleveldb\"\n"), 0o644))

	// No app.toml -> falls back to config.toml.
	b, err := effectiveBackend(home)
	require.NoError(t, err)
	assert.Equal(t, "goleveldb", b)

	// app.toml with empty app-db-backend -> still falls back.
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "app.toml"), []byte("app-db-backend = \"\"\n"), 0o644))
	b, err = effectiveBackend(home)
	require.NoError(t, err)
	assert.Equal(t, "goleveldb", b)

	// app.toml with explicit backend -> takes precedence.
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "app.toml"), []byte("app-db-backend = \"pebbledb\"\n"), 0o644))
	b, err = effectiveBackend(home)
	require.NoError(t, err)
	assert.Equal(t, "pebbledb", b)
}

func TestUpdateBackendConfig_UpdatesBothFiles(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte("# comment\ndb_backend = \"goleveldb\"\nmoniker = \"x\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "app.toml"), []byte("app-db-backend = \"goleveldb\"\nminimum-gas-prices = \"0utia\"\n"), 0o644))

	require.NoError(t, updateBackendConfig(home, "pebbledb"))

	cfg, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(cfg), `db_backend = "pebbledb"`)
	assert.Contains(t, string(cfg), `moniker = "x"`) // other content preserved
	assert.Contains(t, string(cfg), "# comment")

	app, err := os.ReadFile(filepath.Join(home, "config", "app.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(app), `app-db-backend = "pebbledb"`)
	assert.Contains(t, string(app), `minimum-gas-prices = "0utia"`)
}

func TestCheckPassesAfterMigration(t *testing.T) {
	home := setupTestNode(t, 100, 256)
	o := opts(home)
	o.manualSwap = false
	o.backup = false
	require.NoError(t, runMigration(context.Background(), o))

	// Now config reports pebbledb; --check should open and iterate all DBs.
	c := opts(home)
	c.check = true
	require.NoError(t, runMigration(context.Background(), c))
}

// Critical 1: a missing *required* database must abort and never flip config.
func TestMigration_RequiredDBMissingAborts(t *testing.T) {
	home := setupTestNode(t, 50, 128)
	require.NoError(t, os.RemoveAll(filepath.Join(home, "data", "application.db")))

	o := opts(home)
	o.manualSwap = false
	o.backup = false
	o.parallel = 1
	err := runMigration(context.Background(), o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required database not found")

	cfg, rerr := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	require.NoError(t, rerr)
	assert.NotContains(t, string(cfg), "pebbledb", "config must not be flipped when a required DB is missing")
}

// Critical 1 (corollary): a missing *optional* database is tolerated.
func TestMigration_OptionalDBMissingTolerated(t *testing.T) {
	home := setupTestNode(t, 50, 128)
	require.NoError(t, os.RemoveAll(filepath.Join(home, "data", "tx_index.db")))

	o := opts(home)
	o.manualSwap = false
	o.backup = false
	require.NoError(t, runMigration(context.Background(), o))

	cfg, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(cfg), `db_backend = "pebbledb"`)
}

// Critical 2: the deletion count is reported incrementally (per chunk), not only
// at the end, so a crash leaves an accurate lower bound.
func TestCopyAndDeleteKeys_RecordsDeletedPerChunk(t *testing.T) {
	dir := t.TempDir()
	numKeys := 200
	srcDB, err := db.NewDB("src", db.GoLevelDBBackend, dir)
	require.NoError(t, err)
	for i := range numKeys {
		val := make([]byte, 256)
		_, _ = rand.Read(val)
		require.NoError(t, srcDB.Set(fmt.Appendf(nil, "key-%06d", i), val))
	}
	srcIter, err := srcDB.Iterator(nil, nil)
	require.NoError(t, err)
	destDB, err := db.NewDB("dst", db.PebbleDBBackend, dir)
	require.NoError(t, err)

	var recorded int64
	var calls int
	rec := func(n int64) error { recorded += n; calls++; return nil }

	target := dbTarget{name: "test", fileName: "dst", dir: dir}
	smallChunk := int64(10 * 1024) // forces several chunks for 200×256B
	_, _, deleted, err := copyAndDeleteKeys(context.Background(), target, srcDB, destDB, srcIter, 1024*1024, smallChunk, rec, time.Now())
	require.NoError(t, err)
	assert.Equal(t, int64(numKeys), deleted)
	assert.Equal(t, int64(numKeys), recorded, "incrementally recorded total must equal deleted")
	assert.Greater(t, calls, 1, "deletion should be recorded across multiple chunks")

	_ = srcDB.Close()
	_ = destDB.Close()
}

// Critical 2 (crash window): the deletion count is recorded BEFORE the source
// keys are physically removed, so a crash between the two can't undercount.
func TestDeleteChunk_RecordsBeforeDeleting(t *testing.T) {
	dir := t.TempDir()
	srcDB, err := db.NewDB("src", db.GoLevelDBBackend, dir)
	require.NoError(t, err)
	destDB, err := db.NewDB("dst", db.PebbleDBBackend, dir)
	require.NoError(t, err)

	keys := make([][]byte, 0, 50)
	for i := range 50 {
		k := fmt.Appendf(nil, "k-%04d", i)
		require.NoError(t, srcDB.Set(k, []byte("v")))
		require.NoError(t, destDB.Set(k, []byte("v"))) // present in dest so validation passes
		keys = append(keys, k)
	}

	srcCountAtRecord := int64(-1)
	rec := func(n int64) error {
		srcCountAtRecord = countKeys(t, srcDB) // source must still be intact when we record
		return nil
	}
	pdb, _ := destDB.(*db.PebbleDB)
	require.NoError(t, deleteChunk(pdb, srcDB, destDB, keys, "test", rec))

	assert.Equal(t, int64(50), srcCountAtRecord, "count must be recorded before source keys are deleted")
	assert.Equal(t, int64(0), countKeys(t, srcDB), "source keys should be deleted after deleteChunk")

	_ = srcDB.Close()
	_ = destDB.Close()
}

// Critical 2 (stale staging): a fresh run (no state) must refuse to reuse
// leftover .pebble-migrate staging data rather than resume from stale keys.
func TestMigration_RejectsStaleStagingWithoutState(t *testing.T) {
	home := setupTestNode(t, 50, 128)
	// Simulate a prior interrupted run whose data_pebble (state) was removed but
	// whose staging dir survived: create a staged PebbleDB for application.
	stageDir := filepath.Join(home, "data", stagingDirName)
	require.NoError(t, os.MkdirAll(stageDir, 0o755))
	staged, err := db.NewDB("application", db.PebbleDBBackend, stageDir)
	require.NoError(t, err)
	require.NoError(t, staged.Set([]byte("stale"), []byte("data")))
	require.NoError(t, staged.Close())

	err = runMigration(context.Background(), opts(home))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stale staging data")
}

// Critical 3: if the app.toml write fails, config.toml is restored so the two
// files never disagree.
func TestUpdateBackendConfig_RollbackOnAppTomlFailure(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("db_backend = \"goleveldb\"\n"), 0o644))
	// Make app.toml a directory so the write to it fails after config.toml succeeded.
	require.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "app.toml"), 0o755))

	err := updateBackendConfig(home, "pebbledb")
	require.Error(t, err)

	cfg, rerr := os.ReadFile(filepath.Join(cfgDir, "config.toml"))
	require.NoError(t, rerr)
	assert.Contains(t, string(cfg), `db_backend = "goleveldb"`, "config.toml must be restored on app.toml failure")
	assert.NotContains(t, string(cfg), "pebbledb")
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

	target := dbTarget{name: "test", fileName: "dst", dir: dir}
	smallChunk := int64(10 * 1024)
	totalKeys, _, deleted, err := copyAndDeleteKeys(context.Background(), target, srcDB, destDB, srcIter, 1024*1024, smallChunk, nil, time.Now())
	require.NoError(t, err)
	assert.Equal(t, int64(numKeys), totalKeys, "reported key count wrong")
	assert.Equal(t, int64(numKeys), deleted, "reported delete count wrong")
	assert.Equal(t, int64(numKeys), countKeys(t, destDB), "dest key count wrong — keys were lost")

	for k, v := range expected {
		dv, err := destDB.Get([]byte(k))
		require.NoError(t, err)
		assert.True(t, bytes.Equal(v, dv), "value mismatch at %s", k)
	}

	_ = srcDB.Close()
	_ = destDB.Close()
}

func TestCompactPebbleDB(t *testing.T) {
	dir := t.TempDir()
	pdb, err := db.NewDB("compact_test", db.PebbleDBBackend, dir)
	require.NoError(t, err)

	for i := range 500 {
		key := fmt.Appendf(nil, "key-%06d", i)
		val := make([]byte, 512)
		_, _ = rand.Read(val)
		require.NoError(t, pdb.Set(key, val))
	}
	// Include a key that sorts after a 4-byte 0xff sentinel to confirm the
	// compaction range covers the whole keyspace.
	require.NoError(t, pdb.Set([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0x01}, []byte("tail")))

	require.NoError(t, compactPebbleDB("compact_test", pdb))
	assert.Equal(t, int64(501), countKeys(t, pdb))
	require.NoError(t, pdb.Close())
}

func TestCompactPebbleDB_Empty(t *testing.T) {
	dir := t.TempDir()
	pdb, err := db.NewDB("empty", db.PebbleDBBackend, dir)
	require.NoError(t, err)
	require.NoError(t, compactPebbleDB("empty", pdb))
	require.NoError(t, pdb.Close())
}

func TestSourceCompactionAfterDelete(t *testing.T) {
	dir := t.TempDir()
	numKeys := 300
	srcDB, err := db.NewDB("src", db.GoLevelDBBackend, dir)
	require.NoError(t, err)
	for i := range numKeys {
		key := fmt.Appendf(nil, "key-%06d", i)
		val := make([]byte, 256)
		_, _ = rand.Read(val)
		require.NoError(t, srcDB.Set(key, val))
	}

	srcIter, err := srcDB.Iterator(nil, nil)
	require.NoError(t, err)
	destDB, err := db.NewDB("dst", db.PebbleDBBackend, dir)
	require.NoError(t, err)

	target := dbTarget{name: "test", fileName: "dst", dir: dir}
	smallChunk := int64(10 * 1024)
	totalKeys, _, _, err := copyAndDeleteKeys(context.Background(), target, srcDB, destDB, srcIter, 1024*1024, smallChunk, nil, time.Now())
	require.NoError(t, err)
	assert.Equal(t, int64(numKeys), totalKeys)
	assert.Equal(t, int64(numKeys), countKeys(t, destDB))
	assert.Equal(t, int64(0), countKeys(t, srcDB))

	_ = srcDB.Close()
	_ = destDB.Close()
}

func TestIterateCountHash(t *testing.T) {
	dir := t.TempDir()
	a, err := db.NewDB("a", db.PebbleDBBackend, dir)
	require.NoError(t, err)
	b, err := db.NewDB("b", db.GoLevelDBBackend, dir)
	require.NoError(t, err)
	for i := range 100 {
		k := fmt.Appendf(nil, "k-%04d", i)
		v := fmt.Appendf(nil, "v-%04d", i)
		require.NoError(t, a.Set(k, v))
		require.NoError(t, b.Set(k, v))
	}
	ca, ha, err := iterateCountHash(a)
	require.NoError(t, err)
	cb, hb, err := iterateCountHash(b)
	require.NoError(t, err)
	assert.Equal(t, int64(100), ca)
	assert.Equal(t, cb, ca)
	assert.Equal(t, hb, ha, "hashes should match across backends with identical data")

	require.NoError(t, a.Set([]byte("k-0050"), []byte("CHANGED")))
	_, ha2, err := iterateCountHash(a)
	require.NoError(t, err)
	assert.NotEqual(t, hb, ha2, "hash should change when a value changes")
	_ = a.Close()
	_ = b.Close()
}

func TestIsPebbleDB(t *testing.T) {
	dir := t.TempDir()

	levelDir := filepath.Join(dir, "level")
	require.NoError(t, os.MkdirAll(levelDir, 0o755))
	ldb, err := db.NewDB("test", db.GoLevelDBBackend, levelDir)
	require.NoError(t, err)
	require.NoError(t, ldb.Set([]byte("k"), []byte("v")))
	require.NoError(t, ldb.Close())
	assert.False(t, isPebbleDB(filepath.Join(levelDir, "test.db")), "LevelDB should not be detected as PebbleDB")

	pebbleDir := filepath.Join(dir, "pebble")
	require.NoError(t, os.MkdirAll(pebbleDir, 0o755))
	pdb, err := db.NewDB("test", db.PebbleDBBackend, pebbleDir)
	require.NoError(t, err)
	require.NoError(t, pdb.Set([]byte("k"), []byte("v")))
	require.NoError(t, pdb.Close())
	assert.True(t, isPebbleDB(filepath.Join(pebbleDir, "test.db")), "PebbleDB should be detected as PebbleDB")

	assert.False(t, isPebbleDB(filepath.Join(dir, "nonexistent.db")), "non-existent path should return false")
}

func TestMigration_SkipsWhenConfigIsPebbleDB(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"data", "config"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte("db_backend = \"pebbledb\"\n"), 0o644))

	ldb, err := db.NewDB("application", db.GoLevelDBBackend, filepath.Join(home, "data"))
	require.NoError(t, err)
	require.NoError(t, ldb.Set([]byte("k"), []byte("v")))
	require.NoError(t, ldb.Close())

	o := migrateOpts{homeDir: home, backup: true, batchSizeMB: 1, deleteChunkMB: defaultDeleteChunkMB, parallel: 3, manualSwap: true}
	require.NoError(t, runMigration(context.Background(), o))

	_, err = os.Stat(filepath.Join(home, "data", stagingDirName))
	assert.True(t, os.IsNotExist(err), "no staging should be created — migration should have returned early")
}

func TestMigration_ErrorsOnPebbleDBSource(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"data", "config"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte("db_backend = \"goleveldb\"\n"), 0o644))

	for _, name := range testMainDBs {
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
