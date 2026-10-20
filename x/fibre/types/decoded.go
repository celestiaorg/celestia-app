package types

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"

	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DecodedPayForFibre is a MsgPayForFibre tx decoded once per ABCI phase, so
// later steps reuse it instead of deriving it again. Every field comes from
// Raw and must stay immutable for the phase.
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

	hashOnce    sync.Once
	promiseHash [sha256.Size]byte
}

// PromiseKeyed reports whether PromiseKey identifies this promise.
func (d *DecodedPayForFibre) PromiseKeyed() bool {
	return len(d.PromiseSignBytes) > 0
}

// PromiseHash returns the payment promise hash, fibre.PaymentPromise.Hash's
// sha256(SignBytes || Signature), from the sign bytes the decode already
// derived. Nil when the promise is unkeyed or unsigned: the canonical path
// then reports the error.
func (d *DecodedPayForFibre) PromiseHash() []byte {
	if d.Msg == nil || !d.PromiseKeyed() || len(d.Msg.PaymentPromise.Signature) == 0 {
		return nil
	}
	d.hashOnce.Do(func() {
		hash := sha256.New()
		hash.Write(d.PromiseSignBytes)
		hash.Write(d.Msg.PaymentPromise.Signature)
		hash.Sum(d.promiseHash[:0])
	})
	return d.promiseHash[:]
}

// FibreTx returns the tx with its synthesized system blob. Each caller builds
// it at most once, so it is derived rather than memoized: the view then has no
// unguarded mutable field, and the pre-verification pass can share it across
// every CPU.
func (d *DecodedPayForFibre) FibreTx() (*squaretx.FibreTx, error) {
	systemBlob, err := d.Msg.SystemBlob()
	if err != nil {
		return nil, err
	}
	return &squaretx.FibreTx{Tx: d.Raw, SystemBlob: systemBlob}, nil
}

type certKeyContextKey struct{}

// CertKeyHolder carries the decoded view of the transaction being processed
// through a sequential walk, so the context takes one value for the whole walk
// instead of one per transaction. Not safe for concurrent use.
//
// The ante handler reads back the certificate key; the payment promise checks
// read back the promise hash.
type CertKeyHolder struct {
	decoded *DecodedPayForFibre
}

// Set points the holder at the view of the transaction about to be processed.
// A nil view, or one with no bytes, clears the previous one.
func (h *CertKeyHolder) Set(d *DecodedPayForFibre) {
	if d == nil || len(d.Raw) == 0 {
		d = nil
	}
	h.decoded = d
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
	d := DecodedPayForFibreFromContext(ctx)
	if d == nil || !d.CertKeyed {
		return sigcache.Key{}, false
	}
	return d.CertKey, true
}

// DecodedPayForFibreFromContext returns the view attached to ctx, but only if
// it was decoded from exactly ctx.TxBytes(), never one left by another tx.
func DecodedPayForFibreFromContext(ctx sdk.Context) *DecodedPayForFibre {
	if ctx.Context() == nil {
		return nil
	}
	h, _ := ctx.Value(certKeyContextKey{}).(*CertKeyHolder)
	if h == nil || h.decoded == nil || !bytes.Equal(h.decoded.Raw, ctx.TxBytes()) {
		return nil
	}
	return h.decoded
}
