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
// scan followed by a full block cannot evict an entry before PrepareProposal or
// ProcessProposal reads it. Keys are 32 bytes.
const sigCacheCapacity = 3 * (2 * appconsts.MempoolSize / typicalPffTxSize)

// NewSigCache returns the process-wide cache of verified signatures.
func NewSigCache() *sigcache.Cache {
	return sigcache.New(sigCacheCapacity)
}
