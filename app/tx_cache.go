package app

import (
	"crypto/sha256"
	"sync"

	"github.com/celestiaorg/go-square/v4/share"
)

// TxCache caches the transactions
type TxCache struct {
	cache sync.Map
}

// NewTxCache creates a new transaction cache
func NewTxCache() *TxCache {
	return &TxCache{
		cache: sync.Map{},
	}
}

// getTxKey generates a deterministic key for a transaction
func (c *TxCache) getTxKey(tx []byte) string {
	hash := sha256.Sum256(tx)
	return string(hash[:])
}

// Exists checks whether the Tx exists in the cache and the blobs match the cached blobs
func (c *TxCache) Exists(tx []byte, blobs []*share.Blob) bool {
	key := c.getTxKey(tx)
	value, exists := c.cache.Load(key)
	if !exists {
		return false
	}

	cachedBlobHash, ok := value.(string)
	if !ok {
		return false
	}

	blobHash := c.getBlobsHash(blobs)
	return cachedBlobHash == blobHash
}

// Set stores the Tx in the cache
func (c *TxCache) Set(tx []byte, blobs []*share.Blob) {
	key := c.getTxKey(tx)
	blobsHash := c.getBlobsHash(blobs)
	c.cache.Store(key, blobsHash)
}

func (c *TxCache) getBlobsHash(blobs []*share.Blob) string {
	h := sha256.New()
	for _, blob := range blobs {
		h.Write(blob.Namespace().Bytes())
		h.Write(blob.Data())
		h.Write([]byte{blob.ShareVersion()})
		h.Write(blob.Signer())
	}
	sum := h.Sum(nil)
	return string(sum)
}

// RemoveTransaction removes specific transactions from the cache
func (c *TxCache) RemoveTransaction(tx []byte) {
	key := c.getTxKey(tx)
	c.cache.Delete(key)
}

// Size returns the current number of entries in the cache
func (c *TxCache) Size() int {
	count := 0
	c.cache.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}
