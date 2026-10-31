//go:build cgo

package secpverify

import (
	"crypto/sha256"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	dcrsecp "github.com/decred/dcrd/dcrec/secp256k1/v4"
	gethsecp "github.com/ethereum/go-ethereum/crypto/secp256k1"
)

// Native reports whether Verify uses libsecp256k1.
const Native = true

// Verify reports whether sig is a valid R || S signature of sha256(msg) by pub,
// with exactly the verdict of pub.VerifySignature.
func Verify(pub *secp256k1.PubKey, msg, sig []byte) bool {
	if len(sig) != 64 {
		return false
	}
	// decred reduces R and S mod N while libsecp256k1 rejects R, S >= N.
	var r, s dcrsecp.ModNScalar
	r.SetByteSlice(sig[:32])
	s.SetByteSlice(sig[32:])
	var rs [64]byte
	r.PutBytesUnchecked(rs[:32])
	s.PutBytesUnchecked(rs[32:])
	hash := sha256.Sum256(msg)
	return gethsecp.VerifySignature(pub.Key, hash[:], rs[:])
}
