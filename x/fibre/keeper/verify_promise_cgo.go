//go:build cgo && !gofuzz

package keeper

import (
	"crypto/sha256"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	nativesecp "github.com/ethereum/go-ethereum/crypto/secp256k1"
)

// verifyPromise accepts native successes only after all PaymentPromise.Validate
// field checks. Any other input uses the original validator.
func verifyPromise(p *fibre.PaymentPromise) bool {
	if p.SignerKey != nil && len(p.SignerKey.Key) == secp256k1.PubKeySize &&
		(p.SignerKey.Key[0] == 2 || p.SignerKey.Key[0] == 3) &&
		len(p.ChainID) > 0 && len(p.ChainID) <= fibre.MaxChainIDSize &&
		p.UploadSize > 0 && !p.CreationTimestamp.IsZero() &&
		len(p.Signature) == 64 && p.Height > 0 {
		if signBytes, err := p.SignBytes(); err == nil {
			hash := sha256.Sum256(signBytes)
			if nativesecp.VerifySignature(p.SignerKey.Key, hash[:], p.Signature) {
				return true
			}
		}
	}
	return p.Validate() == nil
}
