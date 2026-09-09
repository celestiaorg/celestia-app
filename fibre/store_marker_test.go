package fibre

import (
	"context"
	"encoding/binary"
	"log/slog"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestShardMarkerCodec(t *testing.T) {
	marker := encodeShardMarkerForBackend(localBackendTag, 42)
	require.Equal(t, []byte{1, 1, 0, 0, 0, 0, 0, 0, 0, 42}, marker)

	backend, size, err := decodeShardMarkerBackend(marker)
	require.NoError(t, err)
	require.Equal(t, localBackendTag, backend)
	require.Equal(t, int64(42), size)

	backend, size, err = decodeShardMarkerBackend(nil)
	require.NoError(t, err)
	require.Equal(t, localBackendTag, backend)
	require.Zero(t, size)

	marker = encodeShardMarkerForBackend(objectBackendTag, 42)
	backend, size, err = decodeShardMarkerBackend(marker)
	require.NoError(t, err)
	require.Equal(t, objectBackendTag, backend)
	require.Equal(t, int64(42), size)
}

func TestShardMarkerRejectsInvalidData(t *testing.T) {
	valid := encodeShardMarkerForBackend(localBackendTag, 1)

	overflow := append([]byte(nil), valid...)
	binary.BigEndian.PutUint64(overflow[2:], uint64(math.MaxInt64)+1)
	zeroBackend := append([]byte(nil), valid...)
	zeroBackend[1] = 0
	unknownBackend := append([]byte(nil), valid...)
	unknownBackend[1] = 3
	tests := []struct {
		name string
		data []byte
	}{
		{"truncated", valid[:9]},
		{"overlong", append(valid, 0)},
		{"zero version", append([]byte{0}, valid[1:]...)},
		{"unsupported version", append([]byte{2}, valid[1:]...)},
		{"zero backend", zeroBackend},
		{"unknown backend", unknownBackend},
		{"zero size", []byte{1, 1, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"size overflow", overflow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := decodeShardMarkerBackend(tt.data)
			require.ErrorIs(t, err, ErrStoreIntegrity)
		})
	}
}

func TestStoreSupportsLegacyShardMarker(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	promiseHash := []byte{1, 2, 3}
	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	wantSize := writeMarkerTestShard(t, store, commitment, promiseHash)
	require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), nil, pebbledb.NoSync))
	require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, promiseHash), nil, pebbledb.NoSync))

	shard, err := store.Get(t.Context(), commitment)
	require.NoError(t, err)
	require.Len(t, shard.Rows, 1)
	has, err := store.Has(t.Context(), commitment, promiseHash)
	require.NoError(t, err)
	require.True(t, has)
	size, err := store.Size(t.Context())
	require.NoError(t, err)
	require.Equal(t, wantSize, size)

	pruned, freed, err := store.PruneBefore(t.Context(), pruneAt.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, pruned)
	require.Equal(t, wantSize, freed)
}

func TestStoreRejectsInvalidShardMarkerWithoutDeletingItsData(t *testing.T) {
	tests := []struct {
		name string
		call func(*Store, Commitment, []byte, time.Time) error
	}{
		{"Get", func(s *Store, c Commitment, _ []byte, _ time.Time) error {
			_, err := s.Get(t.Context(), c)
			return err
		}},
		{"Has", func(s *Store, c Commitment, h []byte, _ time.Time) error {
			_, err := s.Has(t.Context(), c, h)
			return err
		}},
		{"Size", func(s *Store, _ Commitment, _ []byte, _ time.Time) error {
			_, err := s.Size(t.Context())
			return err
		}},
		{"PruneBefore", func(s *Store, _ Commitment, _ []byte, pruneAt time.Time) error {
			_, _, err := s.PruneBefore(t.Context(), pruneAt.Add(time.Hour))
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMarkerTestStore(t)
			commitment := generateCommitment()
			promiseHash := []byte{1, 2, 3}
			pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
			writeMarkerTestShard(t, store, commitment, promiseHash)
			invalidMarker := []byte{1, 2, 0, 0, 0, 0, 0, 0, 0, 1}
			require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), invalidMarker, pebbledb.NoSync))
			require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, promiseHash), nil, pebbledb.NoSync))

			require.ErrorIs(t, tt.call(store, commitment, promiseHash, pruneAt), ErrStoreIntegrity)
			local := storeLocalBackend(t, store)
			_, err := local.fs.Stat(local.shardPath(commitment, promiseHash))
			require.NoError(t, err)
			data, closer, err := store.db.Get(shardKey(commitment, promiseHash))
			require.NoError(t, err)
			require.Equal(t, invalidMarker, data)
			require.NoError(t, closer.Close())
		})
	}
}

func TestGetSkipsInvalidMarkerAndReturnsValidShard(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	invalidHash := []byte{1}
	validHash := []byte{2}
	invalidMarker := []byte{1, 2, 0, 0, 0, 0, 0, 0, 0, 1}
	writeMarkerTestShard(t, store, commitment, invalidHash)
	validSize := writeMarkerTestShard(t, store, commitment, validHash)
	validMarker := encodeShardMarkerForBackend(localBackendTag, validSize)
	require.NoError(t, store.db.Set(shardKey(commitment, invalidHash), invalidMarker, pebbledb.NoSync))
	require.NoError(t, store.db.Set(shardKey(commitment, validHash), validMarker, pebbledb.NoSync))

	shard, err := store.Get(t.Context(), commitment)
	require.NoError(t, err)
	require.Equal(t, []byte("data"), shard.Rows[0].Data)
	data, closer, err := store.db.Get(shardKey(commitment, invalidHash))
	require.NoError(t, err)
	require.Equal(t, invalidMarker, data)
	require.NoError(t, closer.Close())
}

func TestGetMissingPayloadKeepsPruneAccounting(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	promiseHash := []byte{1}
	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	size := writeMarkerTestShard(t, store, commitment, promiseHash)
	require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), encodeShardMarkerForBackend(localBackendTag, size), pebbledb.NoSync))
	require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, promiseHash), nil, pebbledb.NoSync))
	local := storeLocalBackend(t, store)
	require.NoError(t, local.fs.Remove(local.shardPath(commitment, promiseHash)))

	_, err := store.Get(t.Context(), commitment)
	require.ErrorIs(t, err, ErrStoreNotFound)

	pruned, freed, err := store.PruneBefore(t.Context(), pruneAt.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, pruned)
	require.Equal(t, size, freed)
}

func TestSizeReturnsValidTotalWithInvalidMarker(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	validHash := []byte{1}
	invalidHash := []byte{2}
	secondInvalidHash := []byte{3}
	validSize := writeMarkerTestShard(t, store, commitment, validHash)
	validMarker := encodeShardMarkerForBackend(localBackendTag, validSize)
	require.NoError(t, store.db.Set(shardKey(commitment, validHash), validMarker, pebbledb.NoSync))
	require.NoError(t, store.db.Set(shardKey(commitment, invalidHash), []byte{1, 2}, pebbledb.NoSync))
	require.NoError(t, store.db.Set(shardKey(commitment, secondInvalidHash), []byte{1, 2}, pebbledb.NoSync))

	size, err := store.Size(t.Context())
	require.ErrorIs(t, err, ErrStoreIntegrity)
	require.Contains(t, err.Error(), "2 invalid shard metadata entries")
	require.Equal(t, validSize, size)
}

func TestSizeRejectsMalformedShardKeyWithValidMarker(t *testing.T) {
	store := newMarkerTestStore(t)
	require.NoError(t, store.db.Set([]byte(shardKeyPrefix+"malformed"), encodeShardMarkerForBackend(localBackendTag, 37), pebbledb.NoSync))

	size, err := store.Size(t.Context())
	require.ErrorIs(t, err, ErrStoreIntegrity)
	require.Zero(t, size)
}

func TestServerSeedsPartialSizeAfterIntegrityError(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	validHash := []byte{1}
	validSize := writeMarkerTestShard(t, store, commitment, validHash)
	validMarker := encodeShardMarkerForBackend(localBackendTag, validSize)
	require.NoError(t, store.db.Set(shardKey(commitment, validHash), validMarker, pebbledb.NoSync))
	require.NoError(t, store.db.Set(shardKey(commitment, []byte{2}), []byte{1, 2}, pebbledb.NoSync))

	var logs strings.Builder
	server := &Server{
		store: store,
		occ:   newOccupancy(0),
		log:   slog.New(slog.NewTextHandler(&logs, nil)),
	}
	require.NoError(t, server.seedOccupancy(t.Context()))
	require.Equal(t, validSize, server.occ.usage())
	require.Contains(t, logs.String(), "store size may be incorrect due to corrupt shard marker")
}

func TestShardStatus(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	promiseHash := []byte{1}

	has, accounted, err := store.shardStatus(t.Context(), commitment, promiseHash)
	require.NoError(t, err)
	require.False(t, has)
	require.False(t, accounted)
	require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), nil, pebbledb.NoSync))
	has, accounted, err = store.shardStatus(t.Context(), commitment, promiseHash)
	require.NoError(t, err)
	require.False(t, has)
	require.False(t, accounted)
	marker := encodeShardMarkerForBackend(localBackendTag, 1)
	require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), marker, pebbledb.NoSync))
	has, accounted, err = store.shardStatus(t.Context(), commitment, promiseHash)
	require.NoError(t, err)
	require.False(t, has)
	require.True(t, accounted)
}

func TestPruneBeforeSkipsInvalidMarkerAndPrunesValidEntry(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	validHash := []byte{1}
	invalidHash := []byte{2}
	validSize := writeMarkerTestShard(t, store, commitment, validHash)
	writeMarkerTestShard(t, store, commitment, invalidHash)
	validMarker := encodeShardMarkerForBackend(localBackendTag, validSize)
	require.NoError(t, store.db.Set(shardKey(commitment, validHash), validMarker, pebbledb.NoSync))
	require.NoError(t, store.db.Set(shardKey(commitment, invalidHash), []byte{1, 2, 0, 0, 0, 0, 0, 0, 0, 1}, pebbledb.NoSync))
	require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, validHash), nil, pebbledb.NoSync))
	require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, invalidHash), nil, pebbledb.NoSync))

	pruned, freed, err := store.PruneBefore(t.Context(), pruneAt.Add(time.Hour))
	require.ErrorIs(t, err, ErrStoreIntegrity)
	require.Equal(t, 1, pruned)
	require.Equal(t, validSize, freed)
	_, closer, err := store.db.Get(shardKey(commitment, validHash))
	require.ErrorIs(t, err, pebbledb.ErrNotFound)
	require.Nil(t, closer)
	local := storeLocalBackend(t, store)
	_, err = local.fs.Stat(local.shardPath(commitment, validHash))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, closer, err = store.db.Get(shardKey(commitment, invalidHash))
	require.NoError(t, err)
	require.NoError(t, closer.Close())
	_, err = local.fs.Stat(local.shardPath(commitment, invalidHash))
	require.NoError(t, err)
}

func TestPruneBeforeHonoursCancellation(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	promiseHash := []byte{1}
	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	size := writeMarkerTestShard(t, store, commitment, promiseHash)
	require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), encodeShardMarkerForBackend(localBackendTag, size), pebbledb.NoSync))
	require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, promiseHash), nil, pebbledb.NoSync))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	pruned, freed, err := store.PruneBefore(ctx, pruneAt.Add(time.Hour))
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, pruned)
	require.Zero(t, freed)
	local := storeLocalBackend(t, store)
	_, err = local.fs.Stat(local.shardPath(commitment, promiseHash))
	require.NoError(t, err)
}

func TestPruneBeforeLimitsBatchSize(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	marker := encodeShardMarkerForBackend(localBackendTag, 1)
	require.NoError(t, store.db.Set(shardKey(commitment, nil), []byte{1, 2}, pebbledb.NoSync))
	require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, nil), nil, pebbledb.NoSync))
	for i := range maxPruneBatchSize + 1 {
		promiseHash := binary.BigEndian.AppendUint64(nil, uint64(i))
		require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), marker, pebbledb.NoSync))
		require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, promiseHash), nil, pebbledb.NoSync))
	}

	pruned, freed, err := store.PruneBefore(t.Context(), pruneAt.Add(time.Hour))
	require.ErrorIs(t, err, ErrStoreIntegrity)
	require.Equal(t, maxPruneBatchSize, pruned)
	require.Equal(t, int64(maxPruneBatchSize), freed)

	pruned, freed, err = store.PruneBefore(t.Context(), pruneAt.Add(time.Hour))
	require.ErrorIs(t, err, ErrStoreIntegrity)
	require.Equal(t, 1, pruned)
	require.Equal(t, int64(1), freed)
}

func TestPruneCommitsProgressOnCancellation(t *testing.T) {
	for _, throughServer := range []bool{false, true} {
		name := "store"
		if throughServer {
			name = "server"
		}
		t.Run(name, func(t *testing.T) {
			store := newMarkerTestStore(t)
			local := storeLocalBackend(t, store)
			commitment := generateCommitment()
			pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
			var size int64
			for _, hash := range [][]byte{{1}, {2}} {
				size = writeMarkerTestShard(t, store, commitment, hash)
				require.NoError(t, store.db.Set(shardKey(commitment, hash), encodeShardMarkerForBackend(localBackendTag, size), pebbledb.NoSync))
				require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, hash), nil, pebbledb.NoSync))
				require.NoError(t, store.db.Set(promiseKey(hash), []byte("promise"), pebbledb.NoSync))
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			store.shards.primary = &cancelAfterDeleteBackend{shardBackend: local, cancel: cancel}
			if throughServer {
				occ := newOccupancy(0)
				occ.seed(2 * size)
				provider := sdkmetric.NewMeterProvider()
				metrics, err := newServerMetrics(provider.Meter("prune-test"), occ)
				require.NoError(t, err)
				server := &Server{store: store, occ: occ, metrics: metrics, log: slog.Default()}
				server.prune(ctx)
				require.Equal(t, size, occ.usage())
			} else {
				pruned, freed, err := store.PruneBefore(ctx, pruneAt.Add(time.Hour))
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, 1, pruned)
				require.Equal(t, size, freed)
			}
			for _, hash := range [][]byte{{1}, {2}} {
				for _, key := range [][]byte{shardKey(commitment, hash), pruneKey(pruneAt, commitment, hash), promiseKey(hash)} {
					_, closer, err := store.db.Get(key)
					if hash[0] == 1 {
						require.ErrorIs(t, err, pebbledb.ErrNotFound)
					} else {
						require.NoError(t, err)
						require.NoError(t, closer.Close())
					}
				}
				_, err := local.fs.Stat(local.shardPath(commitment, hash))
				if hash[0] == 1 {
					require.ErrorIs(t, err, os.ErrNotExist)
				} else {
					require.NoError(t, err)
				}
			}
			pruned, freed, err := store.PruneBefore(t.Context(), pruneAt.Add(time.Hour))
			require.NoError(t, err)
			require.Equal(t, 1, pruned)
			require.Equal(t, size, freed)
		})
	}
}

type cancelAfterDeleteBackend struct {
	shardBackend
	cancel context.CancelFunc
}

func (b *cancelAfterDeleteBackend) Delete(ctx context.Context, commitment Commitment, hash []byte) error {
	if err := b.shardBackend.Delete(ctx, commitment, hash); err != nil {
		return err
	}
	b.cancel()
	return nil
}

func TestPruneBeforeOverflowReturnsNoUncommittedCounts(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	for i, size := range []int64{math.MaxInt64, 1} {
		promiseHash := []byte{byte(i)}
		require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), encodeShardMarkerForBackend(localBackendTag, size), pebbledb.NoSync))
		require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, promiseHash), nil, pebbledb.NoSync))
	}

	pruned, freed, err := store.PruneBefore(t.Context(), pruneAt.Add(time.Hour))
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrStoreIntegrity)
	require.Zero(t, pruned)
	require.Zero(t, freed)
}

func TestServerPruneDrainsBacklog(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	marker := encodeShardMarkerForBackend(localBackendTag, 1)
	require.NoError(t, store.db.Set(shardKey(commitment, nil), []byte{1, 2}, pebbledb.NoSync))
	require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, nil), nil, pebbledb.NoSync))
	for i := range maxPruneBatchSize + 1 {
		promiseHash := binary.BigEndian.AppendUint64(nil, uint64(i))
		require.NoError(t, store.db.Set(shardKey(commitment, promiseHash), marker, pebbledb.NoSync))
		require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, promiseHash), nil, pebbledb.NoSync))
	}

	occ := newOccupancy(0)
	occ.seed(maxPruneBatchSize + 1)
	provider := sdkmetric.NewMeterProvider()
	metrics, err := newServerMetrics(provider.Meter("prune-test"), occ)
	require.NoError(t, err)
	var logs strings.Builder
	server := &Server{store: store, occ: occ, metrics: metrics, log: slog.New(slog.NewTextHandler(&logs, nil))}
	server.prune(t.Context())

	require.Zero(t, occ.usage())
	size, err := store.Size(t.Context())
	require.ErrorIs(t, err, ErrStoreIntegrity)
	require.Zero(t, size)
	require.Contains(t, logs.String(), "prune skipped corrupt shard markers")
	require.Contains(t, logs.String(), "pruned expired entries")
	require.NotContains(t, logs.String(), "level=ERROR")
}

func newMarkerTestStore(t *testing.T) *Store {
	t.Helper()
	store := NewMemoryStore(DefaultStoreConfig())
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

func writeMarkerTestShard(t *testing.T, store *Store, commitment Commitment, promiseHash []byte) int64 {
	t.Helper()
	local := storeLocalBackend(t, store)
	path := local.shardPath(commitment, promiseHash)
	f, err := local.fs.Create(path, shardPayloadWriteCategory)
	require.NoError(t, err)
	require.NoError(t, writeShardBinary(f, &types.BlobShard{
		Rows: []*types.BlobRow{{Index: 1, Data: []byte("data")}},
	}))
	require.NoError(t, f.Close())
	info, err := local.fs.Stat(path)
	require.NoError(t, err)
	return info.Size()
}

func storeLocalBackend(t *testing.T, store *Store) *localBackend {
	t.Helper()
	local, err := store.shards.localBackend()
	require.NoError(t, err)
	return local
}
