package fibre

import (
	"context"
	"encoding/hex"
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
	packID := [packIDSize]byte{1}
	require.NoError(t, db.Set(shardKey(commitment, []byte{5}), encodePackedShardMarker(40, 0, packID), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{6}), encodePackedShardMarker(50, 40, packID), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{7}), encodePackedShardMarker(60, maxPackedObjectBytes, packID), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{8}), encodeShardMarkerForBackend(packedObjectBackendTag, 40), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{9}), encodePackedShardMarker(0, 0, packID), pebbledb.Sync))
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
	require.Equal(t, 9, stats.Markers)
	require.Equal(t, 2, stats.LocalMarkers)
	require.Equal(t, uint64(10), stats.LocalBytes)
	require.Equal(t, 1, stats.ObjectMarkers)
	require.Equal(t, uint64(20), stats.ObjectBytes)
	require.Equal(t, 2, stats.PackedMarkers)
	require.Equal(t, uint64(90), stats.PackedBytes)
	require.Equal(t, 1, stats.LegacyMarkers)
	require.Equal(t, 4, stats.InvalidMarkers)
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

func TestStoreInspectorDoesNotModifyIncompleteStore(t *testing.T) {
	path := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(path, "CURRENT"), nil, 0o600))
	_, err := OpenStoreInspector(path)
	require.Error(t, err)
	entries, err := os.ReadDir(path)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "CURRENT", entries[0].Name())
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
	packID := [packIDSize]byte{0xab}
	require.NoError(t, db.Set(shardKey(commitment, []byte{5}), encodePackedShardMarker(40, 0, packID), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{6}), encodePackedShardMarker(50, maxPackedObjectBytes, packID), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{7}), encodeShardMarkerForBackend(packedObjectBackendTag, 40), pebbledb.Sync))
	require.NoError(t, db.Set(shardKey(commitment, []byte{8}), encodePackedShardMarker(0, 0, packID), pebbledb.Sync))
	require.NoError(t, db.Close())

	inspector, err := OpenStoreInspector(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, inspector.Close()) })

	page, err := inspector.Markers(context.Background(), MarkerFilter{Backend: storageBackendPacked})
	require.NoError(t, err)
	require.Len(t, page.Markers, 1)
	require.Equal(t, storageBackendPacked, page.Markers[0].Backend)
	require.Equal(t, string(shardKey(commitment, []byte{5})), page.Markers[0].Key)
	require.Equal(t, hex.EncodeToString(packID[:]), page.Markers[0].PackID)

	page, err = inspector.Markers(context.Background(), MarkerFilter{Backend: storageBackendObject, Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Markers, 1)
	require.True(t, page.Truncated)
	require.Equal(t, storageBackendObject, page.Markers[0].Backend)
	require.Equal(t, int64(20), page.Markers[0].Size)
	page, err = inspector.Markers(context.Background(), MarkerFilter{Backend: "invalid"})
	require.NoError(t, err)
	require.False(t, page.Truncated)
	wantInvalid := []struct {
		hash byte
		err  string
	}{
		{4, "shard marker length"},
		{6, "invalid packed offset"},
		{7, "shard marker length"},
		{8, "invalid shard size"},
	}
	require.Len(t, page.Markers, len(wantInvalid))
	for i, want := range wantInvalid {
		marker := page.Markers[i]
		require.Equal(t, string(shardKey(commitment, []byte{want.hash})), marker.Key)
		require.Equal(t, "invalid", marker.Backend)
		require.Contains(t, marker.Error, want.err)
		require.Empty(t, marker.PackID)
	}

	_, err = inspector.Markers(context.Background(), MarkerFilter{Backend: "remote"})
	require.ErrorContains(t, err, "backend must be local, object, packed, or invalid")
	_, err = inspector.Markers(context.Background(), MarkerFilter{Limit: -1})
	require.ErrorContains(t, err, "limit must not be negative")
}

func TestInspectPackedMarker(t *testing.T) {
	packID := [packIDSize]byte{0xab, 0xcd}
	tests := []struct {
		name    string
		value   []byte
		backend string
		size    int64
		packID  string
		err     string
	}{
		{name: "valid", value: encodePackedShardMarker(40, maxPackedObjectBytes-40, packID), backend: storageBackendPacked, size: 40, packID: hex.EncodeToString(packID[:])},
		{name: "wrong length", value: encodeShardMarkerForBackend(packedObjectBackendTag, 40), backend: storageBackendInvalid, err: "shard marker length"},
		{name: "zero size", value: encodePackedShardMarker(0, 0, packID), backend: storageBackendInvalid, err: "invalid shard size"},
		{name: "offset past object end", value: encodePackedShardMarker(40, maxPackedObjectBytes-39, packID), backend: storageBackendInvalid, err: "invalid packed offset"},
		{name: "negative offset", value: encodePackedShardMarker(40, -1, packID), backend: storageBackendInvalid, err: "invalid packed offset"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			marker := inspectMarker(shardKey(Commitment{1}, []byte{1}), test.value)
			require.Equal(t, test.backend, marker.Backend)
			require.Equal(t, test.size, marker.Size)
			require.Equal(t, test.packID, marker.PackID)
			require.False(t, marker.Legacy)
			if test.err == "" {
				require.Empty(t, marker.Error)
			} else {
				require.Contains(t, marker.Error, test.err)
			}
		})
	}
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
