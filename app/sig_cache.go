package app

import (
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
)

// typicalPffTxSize is the on-wire size of a MsgPayForFibre transaction carrying
// a 100-validator certificate. Used only to size the signature cache.
const typicalPffTxSize = 7_000

// sigCacheCapacity holds three entries (certificate, promise signature, tx
// signature) for twice as many PFF txs as a full mempool can hold, so a mempool
// scan of PayForFibre traffic followed by a full block cannot evict an entry
// before PrepareProposal or ProcessProposal reads it.
//
// The guarantee is scoped to that traffic. Every transaction the ante handler
// verifies also takes one tx-signature entry in the same LRU, so a mempool of
// small ordinary transfers can push far more entries through the cache than
// this and evict PayForFibre entries early. The cost of an eviction is a
// re-verification, never a wrong answer: the cache holds successes only and
// nothing reads it to decide validity.
//
// Measured cost is about 166 bytes per entry including the LRU list node and
// map overhead - roughly 57 MB at this capacity, not the 11.5 MB the 32-byte
// keys alone suggest.
const sigCacheCapacity = 3 * (2 * appconsts.MempoolSize / typicalPffTxSize)

// NewSigCache returns the process-wide cache of verified signatures.
func NewSigCache() *sigcache.Cache {
	return sigcache.New(sigCacheCapacity)
}

// HasVerifiedSignature reports whether key is in the signature cache. It exists
// so tests can assert what a path did and did not verify.
func (app *App) HasVerifiedSignature(key sigcache.Key) bool {
	return app.sigCache.Has(key)
}
