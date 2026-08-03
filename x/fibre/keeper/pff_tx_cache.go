package keeper

import (
	"crypto/sha256"
	"sync"
)

type sigVerificationCache struct {
	txs sync.Map
}

func newSigVerificationCache() *sigVerificationCache {
	return &sigVerificationCache{}
}

func pffSigCacheKey(tx []byte) [sha256.Size]byte {
	return sha256.Sum256(tx)
}

func (k Keeper) IsPffSigVerificationCached(tx []byte) bool {
	if k.pffSigVerificationCache == nil {
		return false
	}
	_, ok := k.pffSigVerificationCache.txs.Load(pffSigCacheKey(tx))
	return ok
}

func (k Keeper) CachePffSigVerification(tx []byte) {
	if k.pffSigVerificationCache == nil {
		return
	}
	k.pffSigVerificationCache.txs.Store(pffSigCacheKey(tx), struct{}{})
}

func (k Keeper) DeletePffSigVerification(tx []byte) {
	if k.pffSigVerificationCache == nil {
		return
	}
	k.pffSigVerificationCache.txs.Delete(pffSigCacheKey(tx))
}
