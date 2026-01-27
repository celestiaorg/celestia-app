package fibre_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app-fibre/v6/fibre"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
)

func TestStore(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*testing.T, *fibre.Store)
	}{
		{"PutGet_Roundtrip", testStorePutGetRoundtrip},
		{"Put_SameCommitmentSamePromise", testStorePutSameCommitmentSamePromise},
		{"Put_SameCommitmentDifferentPromises", testStorePutSameCommitmentDifferentPromises},
		{"Get_NotFound", testStoreGetNotFound},
		{"PruneBefore_RemovesShardAndPromise", testStorePruneBeforeRemovesShardAndPromise},
		{"PruneBefore_PreservesOtherPromiseShard", testStorePruneBeforePreservesOtherPromiseShard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := makeTestStore(t)
			tt.fn(t, store)
		})
	}
}

func testStorePutGetRoundtrip(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	commitment := blob.Commitment()
	shard := makeShardFrom(t, blob, 0, 1, 2)
	promise := makeTestPaymentPromise(100, commitment)

	// put data
	err := store.Put(ctx, promise, shard, promise.CreationTimestamp)
	require.NoError(t, err)

	// get shard by commitment
	gotShard, err := store.Get(ctx, promise.Commitment)
	require.NoError(t, err)
	require.Len(t, gotShard.Rows, 3)
	require.Equal(t, shard.Rows[0].Index, gotShard.Rows[0].Index)
	require.Equal(t, shard.Rows[1].Index, gotShard.Rows[1].Index)
	require.Equal(t, shard.Rows[2].Index, gotShard.Rows[2].Index)

	// get payment promise by hash
	promiseHash, err := promise.Hash()
	require.NoError(t, err)
	gotPromise, err := store.GetPaymentPromise(ctx, promiseHash)
	require.NoError(t, err)
	require.Equal(t, promise.ChainID, gotPromise.ChainID)
	require.Equal(t, promise.Height, gotPromise.Height)
	require.Equal(t, promise.Commitment, gotPromise.Commitment)
}

func testStorePutSameCommitmentSamePromise(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	commitment := blob.Commitment()
	shard := makeShardFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, commitment)

	// put data first time
	err := store.Put(ctx, promise, shard, promise.CreationTimestamp)
	require.NoError(t, err)

	// put the same data again (same commitment, same promise)
	err = store.Put(ctx, promise, shard, promise.CreationTimestamp)
	require.NoError(t, err)

	// should be able to retrieve
	gotShard, err := store.Get(ctx, commitment)
	require.NoError(t, err)
	require.NotNil(t, gotShard)
	require.Len(t, gotShard.Rows, 2)
}

func testStorePutSameCommitmentDifferentPromises(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	// create a single blob to get the same commitment
	blob := makeTestBlobV0(t, 256)
	commitment := blob.Commitment()

	// extract different shards from the same blob
	shard1 := makeShardFrom(t, blob, 0, 1)
	shard2 := makeShardFrom(t, blob, 2, 3)

	// first promise with rows 0, 1
	promise1 := makeTestPaymentPromise(100, commitment)

	// second promise with different height but same commitment, rows 2, 3
	promise2 := makeTestPaymentPromise(101, commitment)

	// put first promise
	err := store.Put(ctx, promise1, shard1, promise1.CreationTimestamp)
	require.NoError(t, err)

	// put second promise with same commitment but different promise
	err = store.Put(ctx, promise2, shard2, promise2.CreationTimestamp)
	require.NoError(t, err)

	// get should return combined rows from both promises
	gotShard, err := store.Get(ctx, commitment)
	require.NoError(t, err)
	require.NotNil(t, gotShard)
	require.Len(t, gotShard.Rows, 4, "should have all 4 rows from both promises")

	// verify all rows are present
	rowIndices := make(map[uint32]bool)
	for _, row := range gotShard.Rows {
		rowIndices[row.Index] = true
	}
	require.True(t, rowIndices[0], "should have row 0")
	require.True(t, rowIndices[1], "should have row 1")
	require.True(t, rowIndices[2], "should have row 2")
	require.True(t, rowIndices[3], "should have row 3")

	// both promises should be retrievable individually
	hash1, err := promise1.Hash()
	require.NoError(t, err)
	gotPromise1, err := store.GetPaymentPromise(ctx, hash1)
	require.NoError(t, err)
	require.Equal(t, promise1.Height, gotPromise1.Height)

	hash2, err := promise2.Hash()
	require.NoError(t, err)
	gotPromise2, err := store.GetPaymentPromise(ctx, hash2)
	require.NoError(t, err)
	require.Equal(t, promise2.Height, gotPromise2.Height)
}

func testStoreGetNotFound(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	// create commitment that was never stored
	blob := makeTestBlobV0(t, 256)
	commitment := blob.Commitment()

	// try to get commitment that was never stored
	_, err := store.Get(ctx, commitment)
	require.ErrorIs(t, err, fibre.ErrStoreNotFound)
}

func testStorePruneBeforeRemovesShardAndPromise(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	commitment := blob.Commitment()
	shard := makeShardFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, commitment)

	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	cutoffTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	err := store.Put(ctx, promise, shard, pruneAt)
	require.NoError(t, err)

	// verify exists
	promiseHash, _ := promise.Hash()
	_, err = store.GetPaymentPromise(ctx, promiseHash)
	require.NoError(t, err)
	_, err = store.Get(ctx, commitment)
	require.NoError(t, err)

	// prune
	pruned, err := store.PruneBefore(ctx, cutoffTime)
	require.NoError(t, err)
	require.Equal(t, 1, pruned)

	// both promise and shard should be gone
	_, err = store.GetPaymentPromise(ctx, promiseHash)
	require.Error(t, err)
	_, err = store.Get(ctx, commitment)
	require.ErrorIs(t, err, fibre.ErrStoreNotFound)
}

func testStorePruneBeforePreservesOtherPromiseShard(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	commitment := blob.Commitment()

	oldPruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	cutoffTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	newPruneAt := time.Date(2025, 1, 1, 14, 0, 0, 0, time.UTC)

	// two promises with different shards for the same commitment
	oldPromise := makeTestPaymentPromise(100, commitment)
	newPromise := makeTestPaymentPromise(101, commitment)
	oldShard := makeShardFrom(t, blob, 0, 1)
	newShard := makeShardFrom(t, blob, 2, 3)

	err := store.Put(ctx, oldPromise, oldShard, oldPruneAt)
	require.NoError(t, err)
	err = store.Put(ctx, newPromise, newShard, newPruneAt)
	require.NoError(t, err)

	oldHash, _ := oldPromise.Hash()
	newHash, _ := newPromise.Hash()

	// prune old
	pruned, err := store.PruneBefore(ctx, cutoffTime)
	require.NoError(t, err)
	require.Equal(t, 1, pruned)

	// old promise gone
	_, err = store.GetPaymentPromise(ctx, oldHash)
	require.Error(t, err)

	// new promise and its shard still exist
	_, err = store.GetPaymentPromise(ctx, newHash)
	require.NoError(t, err)
	gotShard, err := store.Get(ctx, commitment)
	require.NoError(t, err)
	require.Equal(t, uint32(2), gotShard.Rows[0].Index)
}

func makeTestStore(t *testing.T) *fibre.Store {
	t.Helper()
	cfg := fibre.DefaultStoreConfig()
	cfg.Path = t.TempDir()
	store, err := fibre.NewBadgerStore(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

// makeShardFrom extracts a shard from a blob at the given row indices.
func makeShardFrom(t *testing.T, blob *fibre.Blob, indices ...int) *types.BlobShard {
	t.Helper()

	rows := make([]*types.BlobRow, len(indices))
	for i, idx := range indices {
		rowProof, err := blob.Row(idx)
		require.NoError(t, err)
		rows[i] = &types.BlobRow{
			Index: uint32(idx),
			Data:  rowProof.Row,
			Proof: rowProof.RowProof.RowProof,
		}
	}

	return &types.BlobShard{
		Rows: rows,
		Rlc:  &types.BlobShard_Root{Root: make([]byte, 32)},
	}
}

var testSignerKey = secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey)

// makeTestPaymentPromise creates a test payment promise for store tests.
func makeTestPaymentPromise(height uint64, commitment fibre.Commitment) *fibre.PaymentPromise {
	return &fibre.PaymentPromise{
		ChainID:           "test-chain",
		Height:            height,
		Namespace:         share.MustNewV0Namespace([]byte("test")),
		UploadSize:        1024,
		Commitment:        commitment,
		CreationTimestamp: time.Date(2025, 10, 21, 15, 30, 0, 0, time.UTC),
		SignerKey:         testSignerKey,
		Signature:         []byte("test-signature-64-bytes-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"),
	}
}
