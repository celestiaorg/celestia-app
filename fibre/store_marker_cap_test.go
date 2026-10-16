package fibre

import (
	"testing"

	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

func TestShardMarkerSizePlausibility(t *testing.T) {
	// Match the serialized headers and the decoder's independent per-field bounds.
	const maximum int64 = 12 + shardLengthLimit + int64(maxShardRows)*(12+shardLengthLimit+int64(maxRowProofSegments)*(4+shardLengthLimit))
	for _, backend := range []shardBackendTag{localBackendTag, objectBackendTag} {
		for _, size := range []int64{1, maximum - 1, maximum} {
			gotBackend, gotSize, err := decodeShardMarkerBackend(encodeShardMarkerForBackend(backend, size))
			require.NoError(t, err)
			require.Equal(t, backend, gotBackend)
			require.Equal(t, size, gotSize)
		}
		for _, size := range []int64{maximum + 1, 1 << 62} {
			_, _, err := decodeShardMarkerBackend(encodeShardMarkerForBackend(backend, size))
			require.ErrorIs(t, err, ErrStoreIntegrity)
		}
	}
}

func TestSizeRejectsImplausibleMarkerSize(t *testing.T) {
	store := newMarkerTestStore(t)
	require.NoError(t, store.db.Set(shardKey(Commitment{1}, []byte{1}), encodeShardMarkerForBackend(localBackendTag, 1<<62), pebbledb.NoSync))
	size, err := store.Size(t.Context())
	require.ErrorIs(t, err, ErrStoreIntegrity)
	require.Zero(t, size)
}
