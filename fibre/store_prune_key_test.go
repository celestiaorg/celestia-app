package fibre

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

func TestPruneBeforeRemovesMalformedKeys(t *testing.T) {
	for _, malformed := range []string{
		"/prune/",
		"/prune/short",
		"/prune/000000000000/bad/01",
		"/prune/999999999999/bad/01",
		"/prune/202501011000/bad/01",
		"/prune/not-a-time/" + Commitment{1}.String() + "/01",
	} {
		t.Run(malformed, func(t *testing.T) {
			store := newMarkerTestStore(t)
			var logs strings.Builder
			store.log = slog.New(slog.NewTextHandler(&logs, nil))
			commitment := Commitment{1}
			pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
			size := writeMarkerTestShard(t, store, commitment, []byte{1})
			setPruneEntry(t, store, pruneAt, commitment, []byte{1}, encodeShardMarkerForBackend(localBackendTag, size))
			future := pruneAt.Add(time.Hour)
			setPruneEntry(t, store, future, commitment, []byte{2}, encodeShardMarkerForBackend(localBackendTag, 7))
			require.NoError(t, store.db.Set([]byte(malformed), nil, pebbledb.NoSync))

			pruned, freed, err := store.PruneBefore(t.Context(), future)
			require.NoError(t, err)
			require.Equal(t, 1, pruned)
			require.Equal(t, size, freed)
			requirePruneEntry(t, store, pruneAt, commitment, []byte{1}, false)
			requirePruneEntry(t, store, future, commitment, []byte{2}, true)
			_, closer, err := store.db.Get([]byte(malformed))
			if closer != nil {
				require.NoError(t, closer.Close())
			}
			require.ErrorIs(t, err, pebbledb.ErrNotFound)
			require.Contains(t, logs.String(), "removed malformed prune index keys")
		})
	}
}

func TestPruneMalformedKeysRespectBatchLimit(t *testing.T) {
	store := newMarkerTestStore(t)
	for i := range maxPruneBatchSize + 1 {
		require.NoError(t, store.db.Set(fmt.Appendf(nil, "/prune/000000000000/bad/%04d", i), nil, pebbledb.NoSync))
	}
	before := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	pruned, freed, cursor, err := store.pruneBefore(t.Context(), before, nil)
	var iterCount int
	require.NoError(t, err)
	require.Zero(t, pruned)
	require.Zero(t, freed)
	require.NotEmpty(t, cursor)
	iter, err := store.db.NewIter(nil)
	require.NoError(t, err)
	for valid := iter.First(); valid; valid = iter.Next() {
		iterCount++
	}
	require.NoError(t, iter.Error())
	require.NoError(t, iter.Close())
	require.Equal(t, 1, iterCount)
	pruned, freed, cursor, err = store.pruneBefore(t.Context(), before, cursor)
	require.NoError(t, err)
	require.Zero(t, pruned)
	require.Zero(t, freed)
	require.Empty(t, cursor)
	iter, err = store.db.NewIter(nil)
	require.NoError(t, err)
	require.False(t, iter.First())
	require.NoError(t, iter.Error())
	require.NoError(t, iter.Close())
}

func TestPruneMalformedKeysAfterFutureBatch(t *testing.T) {
	store := newMarkerTestStore(t)
	before := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	future := before.Add(time.Hour)
	commitment := Commitment{1}
	for i := range maxPruneBatchSize {
		setPruneEntry(t, store, future, commitment, []byte(fmt.Sprint(i)), encodeShardMarkerForBackend(localBackendTag, 7))
	}
	malformed := []byte("/prune/not-a-time/bad/01")
	require.NoError(t, store.db.Set(malformed, nil, pebbledb.NoSync))
	pruned, freed, cursor, err := store.pruneBefore(t.Context(), before, nil)
	require.NoError(t, err)
	require.Zero(t, pruned)
	require.Zero(t, freed)
	require.NotEmpty(t, cursor)
	pruned, freed, cursor, err = store.pruneBefore(t.Context(), before, cursor)
	require.NoError(t, err)
	require.Zero(t, pruned)
	require.Zero(t, freed)
	require.Empty(t, cursor)
	_, closer, err := store.db.Get(malformed)
	if closer != nil {
		require.NoError(t, closer.Close())
	}
	require.ErrorIs(t, err, pebbledb.ErrNotFound)
	requirePruneEntry(t, store, future, commitment, []byte("0"), true)
}
