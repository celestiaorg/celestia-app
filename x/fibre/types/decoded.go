package types

import (
	"bytes"
	"context"

	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DecodedPayForFibre is a MsgPayForFibre tx decoded once per ABCI phase, so
// later steps reuse it instead of deriving it again. Every field comes from Raw.
type DecodedPayForFibre struct {
	Raw []byte
	Msg *MsgPayForFibre
	// CertKey is the signature cache key of the validator certificate.
	// CertKeyed is false when the certificate could not be keyed.
	CertKey   sigcache.Key
	CertKeyed bool
	// PromiseSignBytes is what the validators signed, PromiseKey the promise's cache key;
	// both are unset (empty PromiseSignBytes) when the promise is too malformed to key.
	PromiseSignBytes []byte
	PromiseKey       sigcache.Key
}

// PromiseKeyed reports whether PromiseKey identifies this promise.
func (d *DecodedPayForFibre) PromiseKeyed() bool {
	return len(d.PromiseSignBytes) > 0
}

// FibreTx returns the tx with its synthesized system blob. It is derived, not
// memoized, so the view has no mutable field and can be shared across CPUs.
func (d *DecodedPayForFibre) FibreTx() (*squaretx.FibreTx, error) {
	systemBlob, err := d.Msg.SystemBlob()
	if err != nil {
		return nil, err
	}
	return &squaretx.FibreTx{Tx: d.Raw, SystemBlob: systemBlob}, nil
}

type certKeyContextKey struct{}

// CertKeyHolder carries the certificate cache key of the tx a sequential walk is
// processing: one context value per walk. Not safe for concurrent use.
type CertKeyHolder struct {
	raw []byte
	key sigcache.Key
	set bool
}

// Set points the holder at the key derived for the transaction about to be
// processed. A nil view, or one with no usable key, clears the previous one.
func (h *CertKeyHolder) Set(d *DecodedPayForFibre) {
	if d == nil || !d.CertKeyed || len(d.Raw) == 0 {
		*h = CertKeyHolder{}
		return
	}
	*h = CertKeyHolder{raw: d.Raw, key: d.CertKey, set: true}
}

// WithCertKeyHolder attaches h to ctx once, for a whole walk.
func WithCertKeyHolder(ctx sdk.Context, h *CertKeyHolder) sdk.Context {
	if ctx.Context() == nil {
		ctx = ctx.WithContext(context.Background())
	}
	return ctx.WithValue(certKeyContextKey{}, h)
}

// WithCertKey attaches d's certificate key to ctx for a single transaction; a loop
// over many should use [WithCertKeyHolder] so the context chain does not grow.
func WithCertKey(ctx sdk.Context, d *DecodedPayForFibre) sdk.Context {
	h := &CertKeyHolder{}
	h.Set(d)
	return WithCertKeyHolder(ctx, h)
}

// CertKeyFromContext returns the certificate cache key attached to ctx, only if it
// was derived from exactly ctx.TxBytes().
func CertKeyFromContext(ctx sdk.Context) (sigcache.Key, bool) {
	if ctx.Context() == nil {
		return sigcache.Key{}, false
	}
	h, _ := ctx.Value(certKeyContextKey{}).(*CertKeyHolder)
	if h == nil || !h.set || !bytes.Equal(h.raw, ctx.TxBytes()) {
		return sigcache.Key{}, false
	}
	return h.key, true
}
