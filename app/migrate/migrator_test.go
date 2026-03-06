package migrate

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/log"
	cosmosdb "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"
)

func makeSourceDBs(t *testing.T, names []string, keysPerDB int) map[string]cosmosdb.DB {
	t.Helper()
	dbs := make(map[string]cosmosdb.DB, len(names))
	for _, name := range names {
		db := cosmosdb.NewMemDB()
		batch := db.NewBatch()
		for i := 0; i < keysPerDB; i++ {
			key := []byte(fmt.Sprintf("%s-key-%06d", name, i))
			val := []byte(fmt.Sprintf("value-%06d", i))
			require.NoError(t, batch.Set(key, val))
		}
		require.NoError(t, batch.WriteSync())
		batch.Close()
		dbs[name] = db
	}
	return dbs
}

func TestMigrator_Basic(t *testing.T) {
	sources := makeSourceDBs(t, []string{"db1", "db2", "db3"}, 100)
	defer func() {
		for _, db := range sources {
			db.Close()
		}
	}()

	destDir := t.TempDir()
	m := NewMigrator(sources, destDir, 0, log.NewNopLogger())
	err := m.Start(context.Background())
	require.NoError(t, err)

	status := m.Status()
	require.True(t, status.Done)

	// Verify each dest DB has 100 keys.
	for _, name := range []string{"db1", "db2", "db3"} {
		destDB, err := cosmosdb.NewDB(name, cosmosdb.PebbleDBBackend, destDir)
		require.NoError(t, err)
		n := countKeys(t, destDB)
		destDB.Close()
		require.Equal(t, int64(100), n, "db %s should have 100 keys", name)
	}
}

func TestMigrator_Status(t *testing.T) {
	sources := makeSourceDBs(t, []string{"app"}, 200)
	defer func() {
		for _, db := range sources {
			db.Close()
		}
	}()

	destDir := t.TempDir()
	m := NewMigrator(sources, destDir, 0, log.NewNopLogger())

	// Before start, not done.
	require.False(t, m.Status().Done)

	err := m.Start(context.Background())
	require.NoError(t, err)

	status := m.Status()
	require.True(t, status.Done)
	require.True(t, status.Databases["app"].Done)
	require.Equal(t, int64(200), status.Databases["app"].KeysMigrated)
}

func TestMigrator_Cancellation(t *testing.T) {
	sources := makeSourceDBs(t, []string{"big"}, 50000)
	defer func() {
		for _, db := range sources {
			db.Close()
		}
	}()

	destDir := t.TempDir()
	m := NewMigrator(sources, destDir, 0, log.NewNopLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	// Should not panic, may return error or nil depending on timing.
	_ = m.Start(ctx)
}
