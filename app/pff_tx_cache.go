package app

import (
	"bytes"
	"hash/maphash"
	"sync"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	// pffTxCacheCapacity bounds the number of admitted PayForFibre txs
	// remembered for recheck.
	pffTxCacheCapacity = 100_000
	// pffTxCacheMaxBytes bounds the raw tx bytes the cache keeps alive. It
	// matches the mempool, which is the working set a recheck pass walks.
	pffTxCacheMaxBytes = int64(appconsts.MempoolSize)
)

// pffTxKey is a cheap fingerprint of a raw tx. A collision only costs a miss,
// because an entry is used only after its bytes are confirmed identical.
type pffTxKey struct {
	size int
	hash uint64
}

// pffTxEntry is what CheckTx concluded about one exact byte string: the tx
// decodes, carries exactly one MsgPayForFibre, and passes ValidateBasic. raw
// is kept so a lookup can prove the entry belongs to the bytes being checked.
type pffTxEntry struct {
	raw []byte
	tx  sdk.Tx
}

var pffTxSeed = maphash.MakeSeed()

func pffTxFingerprint(raw []byte) pffTxKey {
	return pffTxKey{size: len(raw), hash: maphash.Bytes(pffTxSeed, raw)}
}

// pffTxCache remembers the stateless results CheckTx reached for the exact
// bytes of a MsgPayForFibre tx it admitted. A recheck runs on those same
// bytes, so the decode, shape check and ValidateBasic cannot have changed and
// only the state dependent checks have to run again.
type pffTxCache struct {
	mu       sync.Mutex
	entries  *lru.Cache[pffTxKey, pffTxEntry]
	bytes    int64
	maxBytes int64
}

func newPffTxCache() *pffTxCache {
	return newPffTxCacheSized(pffTxCacheCapacity, pffTxCacheMaxBytes)
}

func newPffTxCacheSized(capacity int, maxBytes int64) *pffTxCache {
	c := &pffTxCache{maxBytes: maxBytes}
	entries, err := lru.NewWithEvict(capacity, func(_ pffTxKey, e pffTxEntry) {
		c.bytes -= int64(len(e.raw))
	})
	if err != nil {
		panic(err)
	}
	c.entries = entries
	return c
}

// Get returns the decoded tx recorded for raw, and only if raw is byte for
// byte the tx it was recorded for.
func (c *pffTxCache) Get(raw []byte) (sdk.Tx, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries.Get(pffTxFingerprint(raw))
	if !ok || !bytes.Equal(raw, entry.raw) {
		return nil, false
	}
	return entry.tx, true
}

// Set records tx as the decoded form of raw.
func (c *pffTxCache) Set(raw []byte, tx sdk.Tx) {
	if len(raw) == 0 || int64(len(raw)) > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := pffTxFingerprint(raw)
	// Replace rather than add, so the eviction callback accounts for the old
	// entry's bytes exactly once.
	c.entries.Remove(key)
	for c.bytes+int64(len(raw)) > c.maxBytes && c.entries.Len() > 0 {
		c.entries.RemoveOldest()
	}
	c.entries.Add(key, pffTxEntry{raw: raw, tx: tx})
	c.bytes += int64(len(raw))
}

// Drop forgets raw. The mempool removes a tx whose recheck fails, so its entry
// would otherwise only leave by eviction.
func (c *pffTxCache) Drop(raw []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := pffTxFingerprint(raw)
	if entry, ok := c.entries.Peek(key); ok && bytes.Equal(raw, entry.raw) {
		c.entries.Remove(key)
	}
}

// Len returns the number of cached txs.
func (c *pffTxCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entries.Len()
}
