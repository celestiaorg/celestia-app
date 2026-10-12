package app

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"

	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
)

// DefaultMempoolMaxTxs mirrors cfg.Mempool.Size from celestia-core's
// DefaultMempoolConfig, which DefaultConsensusConfig does not override. It is
// the fallback when config.toml carries no value, so a node whose config
// predates the key is still sized sensibly. The two drifting apart costs
// nothing beyond a cache that is slightly the wrong size.
const DefaultMempoolMaxTxs = 5_000

// sigCacheEntriesPerTx is how many entries one PayForFibre transaction can take
// in the cache: its certificate, its payment promise and its transaction
// signature. A transaction carrying several PayForFibre messages takes more,
// which the headroom below absorbs.
const sigCacheEntriesPerTx = 3

// sigCacheHeadroomPercent widens the capacity past a full mempool so churn -
// transactions arriving and being evicted between CheckTx and the proposal that
// reads their entries - does not push a live entry out.
const sigCacheHeadroomPercent = 20

// mempoolMaxTxs reports the configured mempool transaction count, the bound that
// decides how many signature cache entries can be live at once. The mempool is
// bounded by both a transaction count and a byte size (appconsts.MempoolSize);
// at any realistic PayForFibre transaction size the count is the binding limit,
// so the cache is sized from it.
func mempoolMaxTxs(appOpts servertypes.AppOptions) int {
	if appOpts == nil {
		return DefaultMempoolMaxTxs
	}
	v := appOpts.Get("mempool.size")
	if v == nil {
		return DefaultMempoolMaxTxs
	}
	// cast.ToInt yields 0 for a missing or unparseable value, and a zero
	// capacity would panic in sigcache.New.
	if size := cast.ToInt(v); size > 0 {
		return size
	}
	return DefaultMempoolMaxTxs
}

// SigCacheCapacity returns the number of entries the signature cache holds for a
// mempool of mempoolMaxTxs transactions: one set of entries per transaction the
// mempool can hold, plus headroom for churn. A mempool scan followed by a full
// block then does not evict an entry before PrepareProposal or ProcessProposal
// reads it.
//
// None of this is consensus critical. The cache holds successes only and nothing
// reads it to decide validity, so an eviction costs a re-verification, never a
// wrong answer. That is why the capacity may be taken from node configuration at
// all: two nodes sized differently still reach the same decisions.
//
// Measured cost is about 166 bytes per entry including the LRU list node and map
// overhead, so the default capacity is roughly 3 MB. The capacity is a ceiling
// rather than an allocation: the underlying map takes no size hint and grows
// with the entries actually stored.
func SigCacheCapacity(mempoolMaxTxs int) int {
	return sigCacheEntriesPerTx * mempoolMaxTxs * (100 + sigCacheHeadroomPercent) / 100
}

// NewSigCache returns the process-wide cache of verified signatures, sized to a
// mempool of mempoolMaxTxs transactions.
func NewSigCache(mempoolMaxTxs int) *sigcache.Cache {
	return sigcache.New(SigCacheCapacity(mempoolMaxTxs))
}
