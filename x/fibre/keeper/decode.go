package keeper

import (
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
)

// DecodePayForFibre decodes a MsgPayForFibre tx once and derives the signature
// cache keys the rest of the phase needs. It returns nil when raw is not a
// pay-for-fibre tx or is too malformed to decode; the validation path then
// reports the error itself.
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
