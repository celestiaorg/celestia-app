//go:build cgo && !gofuzz

package ante

import (
	"crypto/sha256"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	nativesecp "github.com/ethereum/go-ethereum/crypto/secp256k1"
)

// txVerificationPubKey uses native verification for compressed secp256k1 keys.
// Serialization and cache keys continue to use the original SDK key.
func txVerificationPubKey(key cryptotypes.PubKey) cryptotypes.PubKey {
	if key, ok := key.(*secp256k1.PubKey); ok {
		return nativeVerificationKey{key}
	}
	return key
}

type nativeVerificationKey struct {
	*secp256k1.PubKey
}

func (key nativeVerificationKey) VerifySignature(msg, signature []byte) bool {
	if len(key.Key) == secp256k1.PubKeySize && (key.Key[0] == 2 || key.Key[0] == 3) {
		hash := sha256.Sum256(msg)
		if nativesecp.VerifySignature(key.Key, hash[:], signature) {
			return true
		}
	}
	// Preserve SDK acceptance of non-canonical scalar encodings and its
	// behavior for every input rejected by the native verifier.
	return key.PubKey.VerifySignature(msg, signature)
}
