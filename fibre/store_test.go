package fibre_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/fibre"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
)

func TestStore(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*testing.T)
	}{
		{"PutGet_Roundtrip", testStorePutGetRoundtrip},
		{"Put_SameCommitmentSamePromise", testStorePutSameCommitmentSamePromise},
		{"Put_SameCommitmentDifferentPromises", testStorePutSameCommitmentDifferentPromises},
		{"Get_NotFound", testStoreGetNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.fn)
	}
}

func testStorePutGetRoundtrip(t *testing.T) {
	ctx := t.Context()
	store := fibre.NewMemoryStore(fibre.DefaultStoreConfig())

	blob := makeTestBlobV0(t, 256)
	commitment := blob.Commitment()
	shard := makeRowsFrom(t, blob, 0, 1, 2)
	promise := makeTestPaymentPromise(100, commitment)

	// put data
	err := store.Put(ctx, promise, shard)
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

func testStorePutSameCommitmentSamePromise(t *testing.T) {
	ctx := t.Context()
	store := fibre.NewMemoryStore(fibre.DefaultStoreConfig())

	blob := makeTestBlobV0(t, 256)
	commitment := blob.Commitment()
	shard := makeRowsFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, commitment)

	// put data first time
	err := store.Put(ctx, promise, shard)
	require.NoError(t, err)

	// put the same data again (same commitment, same promise)
	err = store.Put(ctx, promise, shard)
	require.NoError(t, err)

	// should be able to retrieve
	gotShard, err := store.Get(ctx, commitment)
	require.NoError(t, err)
	require.NotNil(t, gotShard)
	require.Len(t, gotShard.Rows, 2)
}

func testStorePutSameCommitmentDifferentPromises(t *testing.T) {
	ctx := t.Context()
	store := fibre.NewMemoryStore(fibre.DefaultStoreConfig())

	// create a single blob to get the same commitment
	blob := makeTestBlobV0(t, 256)
	commitment := blob.Commitment()

	// extract different row sets from the same blob
	shard1 := makeRowsFrom(t, blob, 0, 1)
	shard2 := makeRowsFrom(t, blob, 2, 3)

	// first promise with rows 0, 1
	promise1 := makeTestPaymentPromise(100, commitment)

	// second promise with different height but same commitment, rows 2, 3
	promise2 := makeTestPaymentPromise(101, commitment)

	// put first promise
	err := store.Put(ctx, promise1, shard1)
	require.NoError(t, err)

	// put second promise with same commitment but different promise
	err = store.Put(ctx, promise2, shard2)
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

func testStoreGetNotFound(t *testing.T) {
	ctx := t.Context()
	store := fibre.NewMemoryStore(fibre.DefaultStoreConfig())

	// create commitment that was never stored
	blob := makeTestBlobV0(t, 256)
	commitment := blob.Commitment()

	// try to get commitment that was never stored
	_, err := store.Get(ctx, commitment)
	require.ErrorIs(t, err, fibre.ErrStoreNotFound)
}

// makeRowsFrom extracts rows from a blob at the given indices.
func makeRowsFrom(t *testing.T, blob *fibre.Blob, indices ...int) *types.BlobShard {
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

// makeTestPaymentPromise creates a test payment promise for store tests.
func makeTestPaymentPromise(height uint64, commitment fibre.Commitment) *fibre.PaymentPromise {
	return &fibre.PaymentPromise{
		ChainID:           "test-chain",
		Height:            height,
		Namespace:         share.MustNewV0Namespace([]byte("test")),
		UploadSize:        1024,
		Commitment:        commitment,
		CreationTimestamp: time.Date(2025, 10, 21, 15, 30, 0, 0, time.UTC),
		SignerKey:         secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey),
		Signature:         []byte("test-signature-64-bytes-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"),
	}
}
