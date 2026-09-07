package fibre

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRoutedStorageBackend verifies that markers select the configured backend.
func TestRoutedStorageBackend(t *testing.T) {
	primary := &taggedShardBackend{tag: localBackendTag}
	secondary := &taggedShardBackend{tag: objectBackendTag}
	storage := newRoutedStorage(primary, secondary)

	marker := storage.marker(42)
	tag, size, err := decodeShardMarkerBackend(marker)
	require.NoError(t, err)
	require.Equal(t, localBackendTag, tag)
	require.EqualValues(t, 42, size)

	backend, err := storage.backend(localBackendTag)
	require.NoError(t, err)
	require.Same(t, primary, backend)

	backend, err = storage.backend(objectBackendTag)
	require.NoError(t, err)
	require.Same(t, secondary, backend)
	backend, err = storage.backendForMarker(encodeShardMarkerForBackend(objectBackendTag, 42))
	require.NoError(t, err)
	require.Same(t, secondary, backend)

	_, err = storage.backend(shardBackendTag(3))
	require.ErrorIs(t, err, ErrStoreIntegrity)
}

type taggedShardBackend struct {
	shardBackend
	tag shardBackendTag
}

func (b *taggedShardBackend) backendTag() shardBackendTag {
	return b.tag
}
