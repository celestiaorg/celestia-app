package app

import (
	"fmt"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
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
	exists, blobHash := cache.Exists(tx)
	assert.True(t, exists)
	assert.NotEmpty(t, blobHash)
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
		exists, blobHash := cache.Exists(tx)
		assert.True(t, exists)
		assert.NotEmpty(t, blobHash)
	}
}

func TestTxCache_SetDuplicate(t *testing.T) {
	cache := NewTxCache()
	tx := []byte("test transaction")
	blobs := blobfactory.ManyRandBlobs(random.New(), 1000)

	cache.Set(tx, blobs)
	cache.Set(tx, blobs)

	assert.Equal(t, 1, cache.Size())
	exists, blobHash := cache.Exists(tx)
	assert.True(t, exists)
	assert.NotEmpty(t, blobHash)
}

func TestTxCache_Exists(t *testing.T) {
	cache := NewTxCache()
	tx := []byte("test transaction")
	nonExistentTx := []byte("non existent")
	blobs := blobfactory.ManyRandBlobs(random.New(), 1000)

	cache.Set(tx, blobs)

	exists, blobHash := cache.Exists(tx)
	assert.True(t, exists)
	assert.NotEmpty(t, blobHash)
	exists, blobHash = cache.Exists(nonExistentTx)
	assert.False(t, exists)
	assert.Empty(t, blobHash)
}

func TestTxCache_ExistsEmpty(t *testing.T) {
	cache := NewTxCache()
	tx := []byte("test transaction")

	exists, blobHash := cache.Exists(tx)
	assert.False(t, exists)
	assert.Empty(t, blobHash)
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
			exists, blobHash := cache.Exists(transaction)
			require.True(t, exists)
			require.NotEmpty(t, blobHash)
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
		exists, blobHash := cache.Exists(tx)
		require.True(t, exists)
		require.NotEmpty(t, blobHash)
	}
}

func makeBatch(batchNum, size int) [][]byte {
	batch := make([][]byte, size)
	for i := range batch {
		batch[i] = fmt.Appendf([]byte{}, "tx-batch-%d-%d", batchNum, i)
	}
	return batch
}
