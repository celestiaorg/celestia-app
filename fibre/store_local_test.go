package fibre

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cockroachdb/pebble/v2/vfs"
	"github.com/stretchr/testify/require"
)

func TestLocalBackendPayloadLifecycle(t *testing.T) {
	backend, err := newLocalBackend("/store", vfs.NewMem())
	require.NoError(t, err)

	var commitment Commitment
	commitment[0] = 1
	promiseHash := []byte{2}
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Index: 3, Data: []byte("data")}}}

	has, err := backend.Has(t.Context(), commitment, promiseHash)
	require.NoError(t, err)
	require.False(t, has)
	_, err = backend.Get(t.Context(), commitment, promiseHash)
	require.ErrorIs(t, err, ErrStoreNotFound)
	require.NoError(t, backend.Delete(t.Context(), commitment, promiseHash))

	created, err := backend.Put(t.Context(), commitment, promiseHash, shard)
	require.NoError(t, err)
	require.True(t, created)
	created, err = backend.Put(t.Context(), commitment, promiseHash, shard)
	require.NoError(t, err)
	require.True(t, created)

	got, err := backend.Get(t.Context(), commitment, promiseHash)
	require.NoError(t, err)
	require.Equal(t, shard, got)
	has, err = backend.Has(t.Context(), commitment, promiseHash)
	require.NoError(t, err)
	require.True(t, has)

	require.NoError(t, backend.Delete(t.Context(), commitment, promiseHash))
	has, err = backend.Has(t.Context(), commitment, promiseHash)
	require.NoError(t, err)
	require.False(t, has)
}

func TestLocalBackendPutHonoursCancellation(t *testing.T) {
	backend, err := newLocalBackend("/store", vfs.NewMem())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	created, err := backend.Put(ctx, Commitment{}, []byte{1}, &types.BlobShard{})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, created)
	has, err := backend.Has(t.Context(), Commitment{}, []byte{1})
	require.NoError(t, err)
	require.False(t, has)
}
