package app

import (
	"crypto/sha256"
	"sync"
	"time"
)

// TxValidationCache caches the results of expensive transaction validation
// to avoid repeating the same work in ProcessProposal that was already done in CheckTx.
type TxValidationCache struct {
	mu    sync.RWMutex
	cache map[string]*bool
}

// NewTxValidationCache creates a new transaction validation cache
func NewTxValidationCache(ttl time.Duration) *TxValidationCache {
	return &TxValidationCache{
		cache: make(map[string]*bool),
	}
}

// getTxKey generates a deterministic key for a transaction
func (c *TxValidationCache) getTxKey(tx []byte) string {
	hash := sha256.Sum256(tx)
	return string(hash[:])
}

// Get retrieves a validation result from the cache
func (c *TxValidationCache) Get(tx []byte) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := c.getTxKey(tx)
	result, exists := c.cache[key]
	if !exists {
		return false
	}

	return *result
}

// Set stores a validation result in the cache
func (c *TxValidationCache) Set(tx []byte, result bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.getTxKey(tx)
	c.cache[key] = &result
}

// Clear removes all entries from the cache
func (c *TxValidationCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]*bool)
}

// Cleanup removes expired entries from the cache
func (c *TxValidationCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key := range c.cache {
		delete(c.cache, key)
	}
}

// Size returns the current number of entries in the cache
func (c *TxValidationCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}
