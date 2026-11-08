package keeper

import (
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
)

// DecodePayForFibre decodes a MsgPayForFibre tx once and derives its signature
// cache keys. Returns nil if raw is not a decodable pay-for-fibre tx.
func DecodePayForFibre(raw []byte) *types.DecodedPayForFibre {
	return decodePayForFibre(raw, true)
}

// decodePayForFibre omits the certificate key when the caller will not verify it.
func decodePayForFibre(raw []byte, certificate bool) *types.DecodedPayForFibre {
	msg, isPFF, err := types.ParsePayForFibreTx(raw)
	if !isPFF || err != nil {
		return nil
	}
	return derivePayForFibre(raw, msg, certificate)
}

// DerivePayForFibre is DecodePayForFibre for a caller that already holds the
// non-nil message decoded from raw.
func DerivePayForFibre(raw []byte, msg *types.MsgPayForFibre) *types.DecodedPayForFibre {
	return derivePayForFibre(raw, msg, true)
}

func derivePayForFibre(raw []byte, msg *types.MsgPayForFibre, certificate bool) *types.DecodedPayForFibre {
	var certKey sigcache.Key
	var err error
	if certificate {
		certKey, err = msg.SigCacheKey()
	}
	d := DecodedFromMsg(msg, certKey)
	d.Raw, d.CertKeyed = raw, certificate && err == nil
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
