//go:build cgo && !gofuzz

package keeper

import (
	"crypto/sha256"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	nativesecp "github.com/ethereum/go-ethereum/crypto/secp256k1"
)

// verifyPromise is PaymentPromise.Validate with the signature checked by
// VerifySecp256k1.
func verifyPromise(p *fibre.PaymentPromise) bool {
	return p.ValidateWith(VerifySecp256k1) == nil
}

// VerifySecp256k1 reports whether sig is key's signature over msg. It tries
// libsecp256k1 first and the SDK verifier whenever that does not accept, so it
// accepts native ∪ SDK. Every native acceptance is also an SDK one
// (TestNativeSecp256k1AcceptanceIsSubsetOfSDK), so the union is the SDK's set
// and a cgo build accepts exactly what a nocgo build does. The fallback keeps
// the SDK's acceptance of non-canonical scalar encodings, which libsecp256k1
// rejects. It is the one native predicate: app/ante uses it for tx signatures.
func VerifySecp256k1(key *secp256k1.PubKey, msg, sig []byte) bool {
	if len(key.Key) == secp256k1.PubKeySize && (key.Key[0] == 2 || key.Key[0] == 3) {
		hash := sha256.Sum256(msg)
		if nativesecp.VerifySignature(key.Key, hash[:], sig) {
			return true
		}
	}
	return key.VerifySignature(msg, sig)
}
