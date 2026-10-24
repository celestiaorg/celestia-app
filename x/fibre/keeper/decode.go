package keeper

import (
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
)

// DecodePayForFibre decodes a MsgPayForFibre tx once and derives its signature
// cache keys. Returns nil if raw is not a decodable pay-for-fibre tx.
func DecodePayForFibre(raw []byte) *types.DecodedPayForFibre {
	msg, isPFF, err := types.ParsePayForFibreTx(raw)
	if !isPFF || err != nil {
		return nil
	}
	return DerivePayForFibre(raw, msg)
}

// DerivePayForFibre is DecodePayForFibre for a caller that already holds the
// non-nil message decoded from raw.
func DerivePayForFibre(raw []byte, msg *types.MsgPayForFibre) *types.DecodedPayForFibre {
	certKey, err := msg.SigCacheKey()
	d := DecodedFromMsg(msg, certKey)
	d.Raw, d.CertKeyed = raw, err == nil
	return d
}

// DecodedFromMsg builds the decoded view of a message the caller already holds,
// around the certificate key it already derived: DecodePayForFibre without the
// unmarshal. Raw stays nil, so it must never be attached to a context. The promise
// sign bytes and key stay unset when the promise is too malformed to key.
func DecodedFromMsg(msg *types.MsgPayForFibre, certKey sigcache.Key) *types.DecodedPayForFibre {
	d := &types.DecodedPayForFibre{Msg: msg, CertKey: certKey, CertKeyed: true}
	promise := &fibre.PaymentPromise{}
	if err := promise.FromProto(&msg.PaymentPromise); err != nil || promise.SignerKey == nil {
		return d
	}
	signBytes, err := promise.SignBytes()
	if err != nil {
		return d
	}
	d.PromiseSignBytes = signBytes
	d.PromiseKey = promiseSigCacheKeyFromSignBytes(promise, signBytes)
	return d
}
