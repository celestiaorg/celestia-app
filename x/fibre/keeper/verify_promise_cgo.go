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

// VerifySecp256k1 tries libsecp256k1, then the SDK verifier; native acceptance is a
// subset of the SDK's, so cgo and nocgo builds accept the same signatures.
func VerifySecp256k1(key *secp256k1.PubKey, msg, sig []byte) bool {
	if len(key.Key) == secp256k1.PubKeySize && (key.Key[0] == 2 || key.Key[0] == 3) {
		hash := sha256.Sum256(msg)
		if nativesecp.VerifySignature(key.Key, hash[:], sig) {
			return true
		}
	}
	return key.VerifySignature(msg, sig)
}
