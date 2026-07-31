package app

import (
	"crypto/sha256"
)

func pffSigCacheKey(tx []byte) [sha256.Size]byte {
	return sha256.Sum256(tx)
}

func (app *App) isPFFSigCached(tx []byte) bool {
	_, ok := app.pffSignatureVerificationCache.Load(pffSigCacheKey(tx))
	return ok
}

func (app *App) cachePFFSig(tx []byte) {
	app.pffSignatureVerificationCache.Store(pffSigCacheKey(tx), struct{}{})
}
