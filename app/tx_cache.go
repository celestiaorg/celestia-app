package app

import (
	"crypto/sha256"
	"sync"
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

// Exists checks whether the Tx exists in the cache
func (c *TxCache) Exists(tx []byte) (exists bool) {
	key := c.getTxKey(tx)
	_, exists = c.cache.Load(key)
	return exists
}

// Set stores the Tx in the cache
func (c *TxCache) Set(tx []byte) {
	key := c.getTxKey(tx)
	c.cache.Store(key, struct{}{})
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
