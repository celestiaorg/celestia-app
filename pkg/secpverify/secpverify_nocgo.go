//go:build !cgo

package secpverify

import "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"

// Native reports whether Verify uses libsecp256k1.
const Native = false

// Verify reports whether sig is a valid R || S signature of sha256(msg) by pub.
func Verify(pub *secp256k1.PubKey, msg, sig []byte) bool {
	return pub.VerifySignature(msg, sig)
}
