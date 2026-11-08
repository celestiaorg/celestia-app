package validator

import (
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	"github.com/cometbft/cometbft/crypto"
	"sync"
)

// SignatureEvidence is positive cryptographic evidence scoped to one ABCI call.
// It must never be stored in an application cache. Quorum and error ordering
// remain the responsibility of SignatureSet's ordered walk.
type SignatureEvidence struct{ valid sync.Map }

// RecordVerified records only entries of a batch whose entire equation passed.
// Failed, unclaimed and incomplete batches must never call this method.
func (e *SignatureEvidence) RecordVerified(message []byte, keys []crypto.PubKey, signatures [][]byte) {
	if e == nil {
		return
	}
	for i, key := range keys {
		e.valid.Store(evidenceKey(message, key, signatures[i]), struct{}{})
	}
}
func evidenceKey(message []byte, key crypto.PubKey, signature []byte) sigcache.Key {
	return sigcache.NewKey(sigcache.PffCertificate, message, []byte(key.Type()), key.Bytes(), signature)
}
func (e *SignatureEvidence) has(message []byte, key crypto.PubKey, signature []byte) bool {
	if e == nil {
		return false
	}
	_, ok := e.valid.Load(evidenceKey(message, key, signature))
	return ok
}
