// Package secpverify verifies secp256k1 signatures with libsecp256k1 in cgo
// builds and with the SDK's decred verifier otherwise; both give the same verdicts.
package secpverify

import (
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
)

// PubKey returns pk with VerifySignature routed through Verify when pk is a
// secp256k1 key, and pk itself otherwise.
func PubKey(pk cryptotypes.PubKey) cryptotypes.PubKey {
	if k, ok := pk.(*secp256k1.PubKey); ok && Native {
		return pubKey{k}
	}
	return pk
}

type pubKey struct{ *secp256k1.PubKey }

func (k pubKey) VerifySignature(msg, sig []byte) bool { return Verify(k.PubKey, msg, sig) }
