package app

import (
	"bytes"
	"crypto/sha256"

	lru "github.com/hashicorp/golang-lru/v2"
)

// pffTxCacheHeadroomPercent widens the capacity past a full mempool, so churn
// between CheckTx and the recheck that reads an entry does not evict it first.
const pffTxCacheHeadroomPercent = 20

// pffSignerData is the signer address and sequence CheckTx derived for a tx,
// which is everything the recheck path reads back.
type pffSignerData struct {
	addr []byte
	seq  uint64
}

// pffTxCache remembers the signer data CheckTx derived for a MsgPayForFibre tx
// it admitted, so a recheck of those same bytes can skip the decode, the shape
// check and ValidateBasic and run only the state dependent checks.
//
// Entries are keyed by the sha256 of the raw tx, the same identity the mempool
// gives it, so reaching an entry with bytes that were never admitted takes a
// sha256 collision.
type pffTxCache struct {
	entries *lru.Cache[[sha256.Size]byte, pffSignerData]
}

// pffTxCacheCapacity bounds the number of admitted PayForFibre txs remembered
// for recheck, one entry per tx the mempool can hold plus headroom. An entry is
// a fixed ~200 bytes whatever the tx size, so the count is the whole bound.
// Nothing reads the cache to decide validity, so nodes sized differently still
// agree.
func pffTxCacheCapacity(mempoolMaxTxs int) int {
	return mempoolMaxTxs * (100 + pffTxCacheHeadroomPercent) / 100
}

func newPffTxCache(mempoolMaxTxs int) *pffTxCache {
	return newPffTxCacheSized(pffTxCacheCapacity(mempoolMaxTxs))
}

func newPffTxCacheSized(capacity int) *pffTxCache {
	entries, err := lru.New[[sha256.Size]byte, pffSignerData](capacity)
	if err != nil {
		panic(err)
	}
	return &pffTxCache{entries: entries}
}

// Get returns the signer data recorded for raw.
func (c *pffTxCache) Get(raw []byte) (pffSignerData, bool) {
	if len(raw) == 0 {
		return pffSignerData{}, false
	}
	entry, ok := c.entries.Get(sha256.Sum256(raw))
	if !ok {
		return pffSignerData{}, false
	}
	// Hand out a copy, so a caller writing the address into a response cannot
	// reach the cached entry.
	return pffSignerData{addr: bytes.Clone(entry.addr), seq: entry.seq}, true
}

// Set records signer as the signer data CheckTx derived for raw.
func (c *pffTxCache) Set(raw []byte, signer pffSignerData) {
	if len(raw) == 0 || len(signer.addr) == 0 {
		return
	}
	c.entries.Add(sha256.Sum256(raw), pffSignerData{addr: bytes.Clone(signer.addr), seq: signer.seq})
}

// Drop forgets raw, so an entry whose tx just failed a recheck does not sit in
// the cache until it is evicted.
func (c *pffTxCache) Drop(raw []byte) {
	c.entries.Remove(sha256.Sum256(raw))
}

// Len returns the number of cached txs.
func (c *pffTxCache) Len() int {
	return c.entries.Len()
}

// PffTxCacheLen reports how many admitted PayForFibre txs are remembered for
// recheck. What the wrapper records is otherwise invisible from outside this
// package: a hit and a miss reach the same answer by construction, which is
// the point, so tests assert on the cache rather than on the response.
func (app *App) PffTxCacheLen() int {
	return app.pffTxCache.Len()
}
