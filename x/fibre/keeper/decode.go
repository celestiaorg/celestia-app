package keeper

import (
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
)

// DecodePayForFibre decodes a MsgPayForFibre tx once and derives its signature
// cache keys. Returns nil if raw is not a decodable pay-for-fibre tx.
func DecodePayForFibre(raw []byte) *types.DecodedPayForFibre {
	msg, isPFF, err := types.ParsePayForFibreTx(raw)
	if !isPFF || err != nil {
		return nil
	}
	d := &types.DecodedPayForFibre{Raw: raw, Msg: msg}
	if key, err := msg.SigCacheKey(); err == nil {
		d.CertKey, d.CertKeyed = key, true
	}
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
	d.PromiseKeyed = true
	return d
}

// DecodedFromMsg builds the decoded view of a message the caller already holds,
// around the certificate key it already derived: DecodePayForFibre without the
// unmarshal. Raw stays nil, so it must never be attached to a context.
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
	d.PromiseKeyed = true
	return d
}
