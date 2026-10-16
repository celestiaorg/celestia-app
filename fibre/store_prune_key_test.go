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
			// A key sorting after the first future entry is checked on a later pass.
			_, _, err = store.PruneBefore(t.Context(), future.Add(time.Hour))
			require.NoError(t, err)
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
}
