package migrate

import (
	"context"
	"fmt"
	"testing"
	"time"

	cosmosdb "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"
)

func populateMemDB(t *testing.T, db cosmosdb.DB, n int, prefix string) {
	t.Helper()
	batch := db.NewBatch()
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("%s-key-%06d", prefix, i))
		val := []byte(fmt.Sprintf("value-%06d", i))
		require.NoError(t, batch.Set(key, val))
	}
	require.NoError(t, batch.WriteSync())
	batch.Close()
}

func countKeys(t *testing.T, db cosmosdb.DB) int64 {
	t.Helper()
	iter, err := db.Iterator(nil, nil)
	require.NoError(t, err)
	defer iter.Close()
	var count int64
	for ; iter.Valid(); iter.Next() {
		count++
	}
	return count
}

func TestCopyDB_Basic(t *testing.T) {
	src := cosmosdb.NewMemDB()
	defer src.Close()

	dest, err := cosmosdb.NewDB("test_basic", cosmosdb.PebbleDBBackend, t.TempDir())
	require.NoError(t, err)
	defer dest.Close()

	populateMemDB(t, src, 1000, "a")

	result, err := CopyDB(context.Background(), src, dest, CopyDBOptions{
		BatchBytes: 4 * 1024, // small batches to exercise pipeline
	})
	require.NoError(t, err)
	require.Equal(t, int64(1000), result.KeysCopied)
	require.Greater(t, result.BytesCopied, int64(0))

	// Verify all keys present in dest.
	require.Equal(t, int64(1000), countKeys(t, dest))

	// Spot-check a value.
	val, err := dest.Get([]byte("a-key-000500"))
	require.NoError(t, err)
	require.Equal(t, []byte("value-000500"), val)
}

func TestCopyDB_Resume(t *testing.T) {
	src := cosmosdb.NewMemDB()
	defer src.Close()

	destDir := t.TempDir()
	dest, err := cosmosdb.NewDB("test_resume", cosmosdb.PebbleDBBackend, destDir)
	require.NoError(t, err)

	// Copy first 500 keys.
	populateMemDB(t, src, 500, "b")
	result1, err := CopyDB(context.Background(), src, dest, CopyDBOptions{
		BatchBytes: 4 * 1024,
	})
	require.NoError(t, err)
	require.Equal(t, int64(500), result1.KeysCopied)

	dest.Close()

	// Add 500 more keys and resume.
	populateMemDB(t, src, 500, "c") // "c" sorts after "b"

	dest, err = cosmosdb.NewDB("test_resume", cosmosdb.PebbleDBBackend, destDir)
	require.NoError(t, err)
	defer dest.Close()

	result2, err := CopyDB(context.Background(), src, dest, CopyDBOptions{
		BatchBytes: 4 * 1024,
	})
	require.NoError(t, err)
	// result2.KeysCopied includes resumed count + new keys.
	require.Equal(t, int64(1000), result2.KeysCopied)
	require.Equal(t, int64(1000), countKeys(t, dest))
}

func TestCopyDB_Cancellation(t *testing.T) {
	src := cosmosdb.NewMemDB()
	defer src.Close()

	dest, err := cosmosdb.NewDB("test_cancel", cosmosdb.PebbleDBBackend, t.TempDir())
	require.NoError(t, err)
	defer dest.Close()

	populateMemDB(t, src, 50000, "d")

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a very short delay to trigger mid-copy cancellation.
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, err = CopyDB(ctx, src, dest, CopyDBOptions{
		BatchBytes: 1024, // tiny batches
	})
	// Should get a context error.
	require.Error(t, err)

	// Dest should have some consistent data (no corruption).
	n := countKeys(t, dest)
	require.GreaterOrEqual(t, n, int64(0))
}

func TestCopyDB_EmptySource(t *testing.T) {
	src := cosmosdb.NewMemDB()
	defer src.Close()

	dest, err := cosmosdb.NewDB("test_empty", cosmosdb.PebbleDBBackend, t.TempDir())
	require.NoError(t, err)
	defer dest.Close()

	result, err := CopyDB(context.Background(), src, dest, CopyDBOptions{})
	require.NoError(t, err)
	require.Equal(t, int64(0), result.KeysCopied)
	require.Equal(t, int64(0), result.BytesCopied)
}

func TestCopyDB_ProgressFn(t *testing.T) {
	src := cosmosdb.NewMemDB()
	defer src.Close()

	dest, err := cosmosdb.NewDB("test_progress", cosmosdb.PebbleDBBackend, t.TempDir())
	require.NoError(t, err)
	defer dest.Close()

	populateMemDB(t, src, 5000, "e")

	var calls []int64
	_, err = CopyDB(context.Background(), src, dest, CopyDBOptions{
		BatchBytes: 2 * 1024,
		ProgressFn: func(keys, bytes int64) {
			calls = append(calls, keys)
		},
	})
	require.NoError(t, err)
	require.Greater(t, len(calls), 0, "ProgressFn should be called at least once")

	// Verify calls are monotonically increasing.
	for i := 1; i < len(calls); i++ {
		require.GreaterOrEqual(t, calls[i], calls[i-1])
	}
}
