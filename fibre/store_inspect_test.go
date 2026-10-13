package fibre

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

func TestStoreInspectorStats(t *testing.T) {
	path := t.TempDir()
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	require.NoError(t, err)
	commitment := Commitment{1}
	require.NoError(t, db.Set(shardKey(commitment, []byte{1}), encodeShardMarkerForBackend(localBackendTag, 10), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{2}), encodeShardMarkerForBackend(objectBackendTag, 20), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{3}), nil, pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{4}), []byte{1}, pebbledb.Sync))
	now := time.Date(2026, time.October, 13, 12, 0, 30, 0, time.UTC)
	require.NoError(t, db.Set(pruneKey(now.Add(-time.Hour), commitment, []byte{1}), nil, pebbledb.Sync))
	require.NoError(t, db.Set(pruneKey(now, commitment, []byte{2}), nil, pebbledb.Sync))
	require.NoError(t, db.Set([]byte("/prune/bad"), nil, pebbledb.Sync))
	namespace := objectNamespace{
		Endpoint: "https://objects.example.com", Bucket: "fibre", Prefix: "shards",
		ChainID: "celestia", ValidatorAddress: "celestiavalcons1validator",
	}
	require.NoError(t, saveObjectNamespace(db, namespace))
	require.NoError(t, db.Close())

	inspector, err := OpenStoreInspector(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, inspector.Close()) })

	stats, err := inspector.Stats(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, 4, stats.Markers)
	require.Equal(t, 2, stats.LocalMarkers)
	require.Equal(t, uint64(10), stats.LocalBytes)
	require.Equal(t, 1, stats.ObjectMarkers)
	require.Equal(t, uint64(20), stats.ObjectBytes)
	require.Equal(t, 1, stats.LegacyMarkers)
	require.Equal(t, 1, stats.InvalidMarkers)
	require.Equal(t, 3, stats.PruneEntries)
	require.Equal(t, 1, stats.InvalidPruneKeys)
	require.Equal(t, 1, stats.PruneBacklog)
	require.Equal(t, now.Add(-time.Hour).Truncate(time.Minute), stats.OldestPruneAt)
	require.Equal(t, &ObjectNamespaceInfo{
		Endpoint: namespace.Endpoint, Bucket: namespace.Bucket, Prefix: namespace.Prefix,
		ChainID: namespace.ChainID, ValidatorAddress: namespace.ValidatorAddress,
	}, stats.ObjectNamespace)
	require.Empty(t, stats.NamespaceError)

	err = inspector.db.Set([]byte("write"), []byte("blocked"), pebbledb.Sync)
	require.True(t, errors.Is(err, pebbledb.ErrReadOnly))
}

func TestStoreInspectorReportsInvalidObjectNamespace(t *testing.T) {
	path := t.TempDir()
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	require.NoError(t, err)
	require.NoError(t, db.Set([]byte(objectNamespaceKey), []byte("invalid"), pebbledb.Sync))
	require.NoError(t, db.Close())

	inspector, err := OpenStoreInspector(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, inspector.Close()) })
	stats, err := inspector.Stats(t.Context(), time.Now())
	require.NoError(t, err)
	require.Nil(t, stats.ObjectNamespace)
	require.ErrorContains(t, errors.New(stats.NamespaceError), "decoding object namespace")
}

func TestOpenStoreInspectorExplainsLockedStore(t *testing.T) {
	path := t.TempDir()
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	_, err = OpenStoreInspector(path)
	require.ErrorContains(t, err, "stop the Fibre server")
}

func TestStoreInspectorDoesNotCreateMissingPath(t *testing.T) {
	path := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(path, "config"), 0o755))
	_, err := OpenStoreInspector(path)
	require.Error(t, err)
	entries, err := os.ReadDir(path)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "config", entries[0].Name())
}

func TestStoreInspectorMarkersFilterAndLimit(t *testing.T) {
	path := t.TempDir()
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	require.NoError(t, err)
	commitment := Commitment{1}
	require.NoError(t, db.Set(shardKey(commitment, []byte{1}), encodeShardMarkerForBackend(localBackendTag, 10), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{2}), encodeShardMarkerForBackend(objectBackendTag, 20), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{3}), encodeShardMarkerForBackend(objectBackendTag, 30), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{4}), []byte{1}, pebbledb.Sync))
	require.NoError(t, db.Close())

	inspector, err := OpenStoreInspector(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, inspector.Close()) })

	page, err := inspector.Markers(context.Background(), MarkerFilter{Backend: storageBackendObject, Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Markers, 1)
	require.True(t, page.Truncated)
	require.Equal(t, storageBackendObject, page.Markers[0].Backend)
	require.Equal(t, int64(20), page.Markers[0].Size)
	page, err = inspector.Markers(context.Background(), MarkerFilter{Backend: "invalid"})
	require.NoError(t, err)
	require.Len(t, page.Markers, 1)
	require.False(t, page.Truncated)
	require.Equal(t, "invalid", page.Markers[0].Backend)
	require.Contains(t, page.Markers[0].Error, "shard marker length")

	_, err = inspector.Markers(context.Background(), MarkerFilter{Backend: "remote"})
	require.ErrorContains(t, err, "backend must be local, object, or invalid")
	_, err = inspector.Markers(context.Background(), MarkerFilter{Limit: -1})
	require.ErrorContains(t, err, "limit must not be negative")
}

func TestParsePruneTime(t *testing.T) {
	commitment := Commitment{1}
	want := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		key  string
		ok   bool
	}{
		{name: "valid", key: string(pruneKey(want, commitment, []byte{1})), ok: true},
		{name: "bad timestamp", key: "/prune/not-a-time/" + commitment.String() + "/01"},
		{name: "short key", key: "/prune/123456789012"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parsePruneTime(test.key)
			require.Equal(t, test.ok, ok)
			if test.ok {
				require.Equal(t, want, got)
			}
		})
	}
}
