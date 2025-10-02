package app

import (
	"crypto/sha256"
	"sync"
)

// TxValidationCache caches the results of expensive transaction validation
// to avoid repeating the same work in ProcessProposal that was already done in CheckTx.
type TxValidationCache struct {
	cache sync.Map
}

// NewTxValidationCache creates a new transaction validation cache
func NewTxValidationCache() *TxValidationCache {
	return &TxValidationCache{
		cache: sync.Map{},
	}
}

// getTxKey generates a deterministic key for a transaction
func (c *TxValidationCache) getTxKey(tx []byte) string {
	hash := sha256.Sum256(tx)
	return string(hash[:])
}

// Exists retrieves a validation result from the cache
func (c *TxValidationCache) Exists(tx []byte) (exists bool) {
	key := c.getTxKey(tx)
	_, exists = c.cache.Load(key)
	return exists
}

// Set stores a validation result in the cache
func (c *TxValidationCache) Set(tx []byte) {
	key := c.getTxKey(tx)
	c.cache.Store(key, struct{}{})
}

// RemoveTransaction removes specific transactions from the cache
// This is more efficient than clearing everything when only some transactions are finalized
func (c *TxValidationCache) RemoveTransaction(tx []byte) {
	key := c.getTxKey(tx)
	c.cache.Delete(key)
}

// Size returns the current number of entries in the cache
func (c *TxValidationCache) Size() int {
	count := 0
	c.cache.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}
