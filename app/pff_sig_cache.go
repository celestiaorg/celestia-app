package app

import (
	fibreante "github.com/celestiaorg/celestia-app/v10/x/fibre/ante"
	lru "github.com/hashicorp/golang-lru/v2"
)

const defaultPffSigVerificationCacheCapacity = 10_000

// PffSigVerificationCache remembers certificates with verified PFF signatures.
// Its fixed capacity bounds memory use independently of mempool eviction, and
// only certificates that passed verification are admitted, so unverifiable
// spam cannot evict legitimate entries.
type PffSigVerificationCache struct {
	entries *lru.Cache[fibreante.PffSigCacheKey, struct{}]
}

// NewPffSigVerificationCache returns an empty PFF signature cache.
func NewPffSigVerificationCache() *PffSigVerificationCache {
	entries, err := lru.New[fibreante.PffSigCacheKey, struct{}](defaultPffSigVerificationCacheCapacity)
	if err != nil {
		panic(err)
	}
	return &PffSigVerificationCache{entries: entries}
}

// IsCached reports whether key has a cached PFF signature check.
func (c *PffSigVerificationCache) IsCached(key fibreante.PffSigCacheKey) bool {
	_, ok := c.entries.Get(key)
	return ok
}

// Cache marks key as having checked PFF signatures.
func (c *PffSigVerificationCache) Cache(key fibreante.PffSigCacheKey) {
	c.entries.Add(key, struct{}{})
}
