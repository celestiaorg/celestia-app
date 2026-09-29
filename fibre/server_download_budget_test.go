package fibre

import (
	"context"
	"testing"
	"time"

	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

func TestDownloadBudgetAdmit(t *testing.T) {
	budget := newDownloadBudget(10)

	release, err := budget.admit(t.Context(), 6)
	require.NoError(t, err)

	// Not enough budget left: waits until ctx is done.
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = budget.admit(ctx, 6)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// Releasing twice must not over-release.
	release()
	release()
	require.True(t, budget.sem.TryAcquire(10))
	budget.sem.Release(10)

	// An oversized shard is clamped to the capacity so it can still run alone.
	release, err = budget.admit(t.Context(), 100)
	require.NoError(t, err)
	require.False(t, budget.sem.TryAcquire(1))
	release()
	require.True(t, budget.sem.TryAcquire(10))
}

func TestGetAdmittedChargesStoredSize(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	promiseHash := []byte{1}
	size := writeMarkerTestShard(t, store, commitment, promiseHash)
	require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), encodeShardMarkerForBackend(localBackendTag, size), pebbledb.NoSync))

	var admitted int64
	released := 0
	shard, release, err := store.getAdmitted(t.Context(), commitment, func(_ context.Context, n int64) (func(), error) {
		admitted = n
		return func() { released++ }, nil
	})
	require.NoError(t, err)
	require.Equal(t, []byte("data"), shard.Rows[0].Data)
	require.Equal(t, size, admitted)
	require.Zero(t, released, "caller owns the release on success")
	release()
	require.Equal(t, 1, released)

	// An admission failure stops the read.
	_, _, err = store.getAdmitted(t.Context(), commitment, func(context.Context, int64) (func(), error) {
		return nil, context.Canceled
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestGetAdmittedReleasesOnMissingPayload(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	promiseHash := []byte{1}
	size := writeMarkerTestShard(t, store, commitment, promiseHash)
	require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), encodeShardMarkerForBackend(localBackendTag, size), pebbledb.NoSync))
	local := storeLocalBackend(t, store)
	require.NoError(t, local.fs.Remove(local.shardPath(commitment, promiseHash)))

	released := 0
	_, _, err := store.getAdmitted(t.Context(), commitment, func(context.Context, int64) (func(), error) {
		return func() { released++ }, nil
	})
	require.ErrorIs(t, err, ErrStoreNotFound)
	require.Equal(t, 1, released)
}

func TestGetHonoursCancellation(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	promiseHash := []byte{1}
	size := writeMarkerTestShard(t, store, commitment, promiseHash)
	require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), encodeShardMarkerForBackend(localBackendTag, size), pebbledb.NoSync))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := store.Get(ctx, commitment)
	require.ErrorIs(t, err, context.Canceled)
}
