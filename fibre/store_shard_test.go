package fibre

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoutedStorageBackend(t *testing.T) {
	primary := &taggedShardBackend{tag: 1}
	secondary := &taggedShardBackend{tag: 2}
	storage := newRoutedStorage(primary, secondary)

	marker := storage.marker(42)
	tag, size, err := decodeShardMarkerBackend(marker)
	require.NoError(t, err)
	require.Equal(t, byte(1), tag)
	require.EqualValues(t, 42, size)

	backend, err := storage.backend(1)
	require.NoError(t, err)
	require.Same(t, primary, backend)

	backend, err = storage.backend(2)
	require.NoError(t, err)
	require.Same(t, secondary, backend)
	backend, err = storage.backendForMarker(encodeShardMarkerForBackend(2, 42))
	require.NoError(t, err)
	require.Same(t, secondary, backend)

	_, err = storage.backend(3)
	require.ErrorIs(t, err, ErrStoreIntegrity)
}

type taggedShardBackend struct {
	shardBackend
	tag byte
}

func (b *taggedShardBackend) backendTag() byte {
	return b.tag
}
