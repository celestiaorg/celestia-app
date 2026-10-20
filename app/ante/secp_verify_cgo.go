//go:build cgo && !gofuzz

package ante

import (
	fibrekeeper "github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
)

// txVerificationPubKey verifies secp256k1 keys with fibrekeeper.VerifySecp256k1.
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
	return fibrekeeper.VerifySecp256k1(key.PubKey, msg, signature)
}
