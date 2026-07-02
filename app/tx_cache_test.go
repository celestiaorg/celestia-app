package app

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v9/test/util/random"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTxCache(t *testing.T) {
	cache := NewTxCache()
	require.NotNil(t, cache)
	assert.Equal(t, 0, cache.Size())
}

func TestTxCache_Set(t *testing.T) {
	cache := NewTxCache()
	tx := []byte("test transaction")
	blob := blobfactory.ManyRandBlobs(random.New(), 1000)

	cache.Set(tx, blob)
	assert.Equal(t, 1, cache.Size())
	exists := cache.Exists(tx, blob)
	assert.True(t, exists)
}

func TestTxCache_SetMultiple(t *testing.T) {
	cache := NewTxCache()
	txs := [][]byte{
		[]byte("tx1"),
		[]byte("tx2"),
		[]byte("tx3"),
	}
	blobs := blobfactory.ManyRandBlobs(random.New(), 1000)

	for _, tx := range txs {
		cache.Set(tx, blobs)
	}

	assert.Equal(t, 3, cache.Size())
	for _, tx := range txs {
		exists := cache.Exists(tx, blobs)
		assert.True(t, exists)
	}
}

func TestTxCache_SetDuplicate(t *testing.T) {
	cache := NewTxCache()
	tx := []byte("test transaction")
	blobs := blobfactory.ManyRandBlobs(random.New(), 1000)

	cache.Set(tx, blobs)
	cache.Set(tx, blobs)

	assert.Equal(t, 1, cache.Size())
	exists := cache.Exists(tx, blobs)
	assert.True(t, exists)
}

func TestTxCache_Exists(t *testing.T) {
	cache := NewTxCache()
	tx := []byte("test transaction")
	nonExistentTx := []byte("non existent")
	blobs := blobfactory.ManyRandBlobs(random.New(), 1000)

	cache.Set(tx, blobs)

	exists := cache.Exists(tx, blobs)
	assert.True(t, exists)
	exists = cache.Exists(nonExistentTx, blobs)
	assert.False(t, exists)
}

func TestTxCache_ExistsEmpty(t *testing.T) {
	cache := NewTxCache()
	tx := []byte("test transaction")
	blobs := blobfactory.ManyRandBlobs(random.New(), 1000)

	exists := cache.Exists(tx, blobs)
	assert.False(t, exists)
}

func TestTxCache_RemoveTransaction(t *testing.T) {
	cache := NewTxCache()
	tx1 := []byte("tx1")
	tx2 := []byte("tx2")
	tx3 := []byte("tx3")
	blobs1 := blobfactory.ManyRandBlobs(random.New(), 1000)
	blobs2 := blobfactory.ManyRandBlobs(random.New(), 1000)
	blobs3 := blobfactory.ManyRandBlobs(random.New(), 1000)

	cache.Set(tx1, blobs1)
	cache.Set(tx2, blobs2)
	cache.Set(tx3, blobs3)
	assert.Equal(t, 3, cache.Size())

	cache.RemoveTransaction(tx2)

	assert.Equal(t, 2, cache.Size())
	exists := cache.Exists(tx1, blobs1)
	assert.True(t, exists)

	exists = cache.Exists(tx2, blobs2)
	assert.False(t, exists)

	exists = cache.Exists(tx3, blobs3)
	assert.True(t, exists)
}

func TestTxCache_RemoveTransactionNonExistent(t *testing.T) {
	cache := NewTxCache()
	tx := []byte("tx1")
	blobs := blobfactory.ManyRandBlobs(random.New(), 1000)
	nonExistentTx := []byte("non existent")

	cache.Set(tx, blobs)
	assert.Equal(t, 1, cache.Size())

	cache.RemoveTransaction(nonExistentTx)
	assert.Equal(t, 1, cache.Size())
	exists := cache.Exists(tx, blobs)
	assert.True(t, exists)
}

func TestTxCache_GetTxKey(t *testing.T) {
	cache := NewTxCache()
	tx := []byte("test transaction")

	// same transaction should produce same key
	key1 := cache.getTxKey(tx)
	key2 := cache.getTxKey(tx)
	assert.Equal(t, key1, key2)

	// different transactions should produce different keys
	tx2 := []byte("different transaction")
	key3 := cache.getTxKey(tx2)
	assert.NotEqual(t, key1, key3)
}

func TestTxCache_GetBlobsHash(t *testing.T) {
	cache := NewTxCache()
	blobs := blobfactory.ManyRandBlobs(random.New(), 1000)

	// same blobs should produce same hash
	blobsHash1 := cache.getBlobsHash(blobs)
	blobsHash2 := cache.getBlobsHash(blobs)
	assert.Equal(t, blobsHash1, blobsHash2)

	// different blobs should produce different hashes
	blobs2 := blobfactory.ManyRandBlobs(random.New(), 1000)
	blobsHash3 := cache.getBlobsHash(blobs2)
	assert.NotEqual(t, blobsHash1, blobsHash3)
}

// TestTxCache_GetBlobsHashCrossVersionCollision is a regression test for the
// collision pattern in GHSA-jv5p-xwcc-q8mf: without domain separation and
// length prefixes, a v0 blob whose data embeds the v1 share version byte and
// signer hashes identically to the v1 blob (when the signer ends in 0x00).
func TestTxCache_GetBlobsHashCrossVersionCollision(t *testing.T) {
	cache := NewTxCache()
	ns := share.RandomNamespace()

	victimData := []byte("victim payload")
	victimSigner := append(bytes.Repeat([]byte{0xAA}, share.SignerSize-1), 0x00)
	v1Blob, err := share.NewV1Blob(ns, victimData, victimSigner)
	require.NoError(t, err)

	attackerData := append([]byte{}, victimData...)
	attackerData = append(attackerData, share.ShareVersionOne)
	attackerData = append(attackerData, victimSigner[:share.SignerSize-1]...)
	v0Blob, err := share.NewV0Blob(ns, attackerData)
	require.NoError(t, err)

	assert.NotEqual(t, cache.getBlobsHash([]*share.Blob{v1Blob}), cache.getBlobsHash([]*share.Blob{v0Blob}),
		"a v0 blob must not collide with a v1 blob's hash")
}

// TestTxCache_GetBlobsHashInterBlobBoundaryCollision asserts that the boundary
// between adjacent blobs is pinned: two v0 blobs must not hash identically to
// a single v0 blob whose data embeds the concatenation of both.
func TestTxCache_GetBlobsHashInterBlobBoundaryCollision(t *testing.T) {
	cache := NewTxCache()
	ns := share.RandomNamespace()

	blob1, err := share.NewV0Blob(ns, []byte("first blob"))
	require.NoError(t, err)
	blob2, err := share.NewV0Blob(ns, []byte("second blob"))
	require.NoError(t, err)

	// merged.data = blob1.data || v0 share version byte || ns || blob2.data, so
	// the ambiguous concatenation produces the same byte stream as [blob1, blob2].
	mergedData := append([]byte{}, blob1.Data()...)
	mergedData = append(mergedData, share.ShareVersionZero)
	mergedData = append(mergedData, ns.Bytes()...)
	mergedData = append(mergedData, blob2.Data()...)
	merged, err := share.NewV0Blob(ns, mergedData)
	require.NoError(t, err)

	assert.NotEqual(t, cache.getBlobsHash([]*share.Blob{blob1, blob2}), cache.getBlobsHash([]*share.Blob{merged}),
		"adjacent blobs must not collide with a single merged blob")
}

func TestTxCache_GetTxKeyEmptyTx(t *testing.T) {
	cache := NewTxCache()
	emptyTx := []byte{}

	// Should handle empty transaction
	key := cache.getTxKey(emptyTx)
	assert.NotEmpty(t, key)
}

func TestTxCache_ConcurrentSet(t *testing.T) {
	cache := NewTxCache()
	numGoroutines := 100
	numTxsPerGoroutine := 100

	blobs := blobfactory.ManyRandBlobs(random.New(), 100000)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			for j := range numTxsPerGoroutine {
				tx := []byte{byte(id), byte(j)}
				cache.Set(tx, blobs)
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, numGoroutines*numTxsPerGoroutine, cache.Size())
}

func TestTxCache_ConcurrentBatches(t *testing.T) {
	cache := NewTxCache()

	batch1 := makeBatch(1, 100)
	batch2 := makeBatch(2, 200)
	batch3 := makeBatch(3, 150)

	blobs := blobfactory.ManyRandBlobs(random.New(), 100000)

	// phase 1: Write batch 1 sequentially
	for _, tx := range batch1 {
		cache.Set(tx, blobs)
	}

	// phase 2: Concurrently write batch 2 and check batch 1 exists
	var wg sync.WaitGroup

	// write batch 2 concurrently
	for _, tx := range batch2 {
		wg.Add(1)
		go func(transaction []byte) {
			defer wg.Done()
			cache.Set(transaction, blobs)
		}(tx)
	}

	// Check batch 1 exists concurrently
	for _, tx := range batch1 {
		wg.Add(1)
		go func(transaction []byte) {
			defer wg.Done()
			exists := cache.Exists(transaction, blobs)
			require.True(t, exists)
		}(tx)
	}

	wg.Wait()

	// check size equals batch 2 size (batch 1 and 2 are different, so size should be batch1 + batch2)
	expectedSize := len(batch1) + len(batch2)
	require.Equal(t, expectedSize, cache.Size())

	// phase 3: Concurrently remove batch 2 and add batch 3
	// remove batch 2
	for _, tx := range batch2 {
		wg.Add(1)
		go func(transaction []byte) {
			defer wg.Done()
			cache.RemoveTransaction(transaction)
		}(tx)
	}

	// add batch 3
	for _, tx := range batch3 {
		wg.Add(1)
		go func(transaction []byte) {
			defer wg.Done()
			cache.Set(transaction, blobs)
		}(tx)
	}

	wg.Wait()

	// phase 4: Check batch 3 exists sequentially
	for _, tx := range batch3 {
		exists := cache.Exists(tx, blobs)
		require.True(t, exists)
	}
}

func makeBatch(batchNum, size int) [][]byte {
	batch := make([][]byte, size)
	for i := range batch {
		batch[i] = fmt.Appendf([]byte{}, "tx-batch-%d-%d", batchNum, i)
	}
	return batch
}
