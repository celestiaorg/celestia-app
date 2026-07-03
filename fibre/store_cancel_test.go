package fibre_test

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/stretchr/testify/require"
)

// A cancelled request context must abort Put before the pebble commit: nothing
// is published or committed, and no staged tmp file is left behind. This is the
// server honoring a client cancellation and stopping the commit while the data
// is still only staged.
func TestStorePutHonorsCancellation(t *testing.T) {
	store, path := makeTestStore(t)

	blob := makeTestBlobV0(t, 256)
	shard := makeShardFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, blob.ID())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Put(ctx, promise, shard, promise.CreationTimestamp)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	// Not committed: the commitment is not discoverable.
	_, err = store.Get(context.Background(), blob.ID().Commitment())
	require.ErrorIs(t, err, fibre.ErrStoreNotFound)

	// No shard file was published under shards/.
	promiseHash, err := promise.Hash()
	require.NoError(t, err)
	shardFile := filepath.Join(path, "shards", blob.ID().Commitment().String()+"-"+hex.EncodeToString(promiseHash))
	_, statErr := os.Stat(shardFile)
	require.True(t, os.IsNotExist(statErr), "no shard file should be published on cancel")

	// No staged tmp leaked under staging/.
	entries, err := os.ReadDir(filepath.Join(path, "staging"))
	require.NoError(t, err)
	require.Empty(t, entries, "staging must be clean after a cancelled Put")

	// A subsequent, uncancelled Put of the same data still succeeds and is
	// discoverable — the aborted attempt left no poisoning state behind.
	require.NoError(t, store.Put(context.Background(), promise, shard, promise.CreationTimestamp))
	got, err := store.Get(context.Background(), blob.ID().Commitment())
	require.NoError(t, err)
	require.Len(t, got.Rows, 2)
}
