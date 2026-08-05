package app

import (
	"crypto/sha256"
	"sync"
)

// PffSigVerificationCache remembers txs with verified PFF signatures.
type PffSigVerificationCache struct {
	txs sync.Map
}

// NewPffSigVerificationCache returns an empty PFF signature cache.
func NewPffSigVerificationCache() *PffSigVerificationCache {
	return &PffSigVerificationCache{
		txs: sync.Map{},
	}
}

func pffSigCacheKey(tx []byte) [sha256.Size]byte {
	return sha256.Sum256(tx)
}

// IsCached reports whether tx has a cached PFF signature check.
func (c *PffSigVerificationCache) IsCached(tx []byte) bool {
	_, ok := c.txs.Load(pffSigCacheKey(tx))
	return ok
}

// Cache marks tx as having checked PFF signatures.
func (c *PffSigVerificationCache) Cache(tx []byte) {
	c.txs.Store(pffSigCacheKey(tx), struct{}{})
}

// Delete removes tx from the PFF signature cache.
func (c *PffSigVerificationCache) Delete(tx []byte) {
	c.txs.Delete(pffSigCacheKey(tx))
}
