package app

import (
	"container/list"
	"sync"

	fibreante "github.com/celestiaorg/celestia-app/v10/x/fibre/ante"
)

const defaultPffSigVerificationCacheCapacity = 10_000

// PffSigVerificationCache remembers certificates with verified PFF signatures.
// Its fixed capacity bounds memory use independently of mempool eviction.
type PffSigVerificationCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[fibreante.PffSigCacheKey]*list.Element
	lru      *list.List
}

// NewPffSigVerificationCache returns an empty PFF signature cache.
func NewPffSigVerificationCache() *PffSigVerificationCache {
	return newPffSigVerificationCache(defaultPffSigVerificationCacheCapacity)
}

func newPffSigVerificationCache(capacity int) *PffSigVerificationCache {
	if capacity <= 0 {
		panic("PFF signature cache capacity must be positive")
	}
	return &PffSigVerificationCache{
		capacity: capacity,
		entries:  make(map[fibreante.PffSigCacheKey]*list.Element, capacity),
		lru:      list.New(),
	}
}

// IsCached reports whether key has a cached PFF signature check.
func (c *PffSigVerificationCache) IsCached(key fibreante.PffSigCacheKey) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.entries[key]
	if ok {
		c.lru.MoveToFront(element)
	}
	return ok
}

// Cache marks key as having checked PFF signatures.
func (c *PffSigVerificationCache) Cache(key fibreante.PffSigCacheKey) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, ok := c.entries[key]; ok {
		c.lru.MoveToFront(element)
		return
	}

	element := c.lru.PushFront(key)
	c.entries[key] = element
	if c.lru.Len() <= c.capacity {
		return
	}

	oldest := c.lru.Back()
	delete(c.entries, oldest.Value.(fibreante.PffSigCacheKey))
	c.lru.Remove(oldest)
}
