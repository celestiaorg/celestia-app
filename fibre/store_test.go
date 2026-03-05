package fibre_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app-fibre/v6/fibre"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
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
		{"Get_DeterministicOrdering", testStoreGetDeterministicOrdering},
		{"PruneBefore_RemovesShardAndPromise", testStorePruneBeforeRemovesShardAndPromise},
		{"PruneBefore_PreservesOtherPromiseShard", testStorePruneBeforePreservesOtherPromiseShard},
		{"PruneBefore_NonUTCCutoff_DoesNotPruneUnexpired", testStorePruneBeforeNonUTCCutoffDoesNotPruneUnexpired},
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
	shard := makeShardFrom(t, blob, 0, 1, 2)
	promise := makeTestPaymentPromise(100, blob.ID())

	// put data
	err := store.Put(ctx, promise, shard, promise.CreationTimestamp)
	require.NoError(t, err)

	// get shard by commitment
	gotShard, err := store.Get(ctx, blob.ID().Commitment())
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
	shard := makeShardFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, blob.ID())

	// put data first time
	err := store.Put(ctx, promise, shard, promise.CreationTimestamp)
	require.NoError(t, err)

	// put the same data again (same commitment, same promise)
	err = store.Put(ctx, promise, shard, promise.CreationTimestamp)
	require.NoError(t, err)

	// should be able to retrieve
	gotShard, err := store.Get(ctx, blob.ID().Commitment())
	require.NoError(t, err)
	require.NotNil(t, gotShard)
	require.Len(t, gotShard.Rows, 2)
}

func testStorePutSameCommitmentDifferentPromises(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	// create a single blob to get the same commitment
	blob := makeTestBlobV0(t, 256)

	// extract different shards from the same blob
	shard1 := makeShardFrom(t, blob, 0, 1)
	shard2 := makeShardFrom(t, blob, 2, 3)

	// first promise with rows 0, 1
	promise1 := makeTestPaymentPromise(100, blob.ID())

	// second promise with different height but same commitment, rows 2, 3
	promise2 := makeTestPaymentPromise(101, blob.ID())

	// put first promise
	err := store.Put(ctx, promise1, shard1, promise1.CreationTimestamp)
	require.NoError(t, err)

	// put second promise with same commitment but different promise
	err = store.Put(ctx, promise2, shard2, promise2.CreationTimestamp)
	require.NoError(t, err)

	// get returns only first shard found (to prevent unbounded message sizes)
	gotShard, err := store.Get(ctx, blob.ID().Commitment())
	require.NoError(t, err)
	require.NotNil(t, gotShard)
	require.Len(t, gotShard.Rows, 2, "should have 2 rows from first shard")

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

	// try to get commitment that was never stored
	_, err := store.Get(ctx, blob.ID().Commitment())
	require.ErrorIs(t, err, fibre.ErrStoreNotFound)
}

func testStorePruneBeforeRemovesShardAndPromise(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	shard := makeShardFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, blob.ID())

	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	cutoffTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	err := store.Put(ctx, promise, shard, pruneAt)
	require.NoError(t, err)

	// verify exists
	promiseHash, _ := promise.Hash()
	_, err = store.GetPaymentPromise(ctx, promiseHash)
	require.NoError(t, err)
	_, err = store.Get(ctx, blob.ID().Commitment())
	require.NoError(t, err)

	// prune
	pruned, err := store.PruneBefore(ctx, cutoffTime)
	require.NoError(t, err)
	require.Equal(t, 1, pruned)

	// both promise and shard should be gone
	_, err = store.GetPaymentPromise(ctx, promiseHash)
	require.Error(t, err)
	_, err = store.Get(ctx, blob.ID().Commitment())
	require.ErrorIs(t, err, fibre.ErrStoreNotFound)
}

func testStorePruneBeforePreservesOtherPromiseShard(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)

	oldPruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	cutoffTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	newPruneAt := time.Date(2025, 1, 1, 14, 0, 0, 0, time.UTC)

	// two promises with different shards for the same commitment
	oldPromise := makeTestPaymentPromise(100, blob.ID())
	newPromise := makeTestPaymentPromise(101, blob.ID())
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
	gotShard, err := store.Get(ctx, blob.ID().Commitment())
	require.NoError(t, err)
	require.Equal(t, uint32(2), gotShard.Rows[0].Index)
}

// testStorePruneBeforeNonUTCCutoffDoesNotPruneUnexpired is a regression test for a timezone bug
// where PruneBefore would incorrectly prune entries on non-UTC machines.
func testStorePruneBeforeNonUTCCutoffDoesNotPruneUnexpired(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	shard := makeShardFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, blob.ID())

	// entry expires at 19:08 UTC
	pruneAt := time.Date(2025, 1, 1, 19, 8, 0, 0, time.UTC)
	err := store.Put(ctx, promise, shard, pruneAt)
	require.NoError(t, err)

	promiseHash, _ := promise.Hash()

	// cutoff is 19:11 UTC+1 = 18:11 UTC, which is before the entry's expiry of 19:08 UTC.
	// before the fix, PruneBefore compared the UTC key "202501011908" against the local-formatted
	// string "202501011911", making the entry appear expired and pruning it incorrectly.
	utcPlusOne := time.FixedZone("UTC+1", 60*60)
	cutoff := time.Date(2025, 1, 1, 19, 11, 0, 0, utcPlusOne)

	pruned, err := store.PruneBefore(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, 0, pruned)

	_, err = store.GetPaymentPromise(ctx, promiseHash)
	require.NoError(t, err)
}

func testStoreGetDeterministicOrdering(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	id := blob.ID()

	// store multiple shards with different row indices
	for i := range 5 {
		shard := makeShardFrom(t, blob, i*2, i*2+1)
		promise := makeTestPaymentPromise(uint64(100+i), id)
		err := store.Put(ctx, promise, shard, promise.CreationTimestamp)
		require.NoError(t, err)
	}

	// get multiple times and verify ordering is deterministic
	var firstRowIndex uint32
	for i := range 10 {
		gotShard, err := store.Get(ctx, id.Commitment())
		require.NoError(t, err)
		require.NotEmpty(t, gotShard.Rows)

		if i == 0 {
			firstRowIndex = gotShard.Rows[0].Index
		} else {
			require.Equal(t, firstRowIndex, gotShard.Rows[0].Index,
				"query ordering should be deterministic - first row index should always be the same")
		}
	}
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
func makeTestPaymentPromise(height uint64, blobID fibre.BlobID) *fibre.PaymentPromise {
	return &fibre.PaymentPromise{
		ChainID:           "test-chain",
		Height:            height,
		Namespace:         testNamespace,
		UploadSize:        1024,
		BlobVersion:       uint32(blobID.Version()),
		Commitment:        blobID.Commitment(),
		CreationTimestamp: time.Date(2025, 10, 21, 15, 30, 0, 0, time.UTC),
		SignerKey:         testSignerKey,
		Signature:         []byte("test-signature-64-bytes-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"),
	}
}
