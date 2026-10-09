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

// DecodedPayForFibre is a MsgPayForFibre tx decoded once per ABCI phase, with
// the values later steps of that phase would otherwise derive again from the
// same bytes. Raw, Msg, and the derived verification inputs must remain
// immutable for the phase.
type DecodedPayForFibre struct {
	Raw []byte
	Msg *MsgPayForFibre
	// CertKey is the signature cache key of the validator certificate.
	// CertKeyed is false when the certificate could not be keyed.
	CertKey   sigcache.Key
	CertKeyed bool
	// PromiseSignBytes is what the validators signed and PromiseKey the
	// signature cache key of the promise. PromiseKeyed is false when the
	// promise is too malformed to derive them; validation then reports it.
	PromiseSignBytes []byte
	PromiseKey       sigcache.Key
	PromiseKeyed     bool
	// GasLimit is the gas the transaction declared, read at decode time. Zero
	// when the auth info could not be read.
	GasLimit uint64

	// fibreTx is built on first use and guarded: the proposal square is built
	// on a goroutine that runs alongside the ordered walk, and both may reach
	// it.
	fibreTxOnce sync.Once
	fibreTx     *squaretx.FibreTx
	fibreTxErr  error
	hashOnce    sync.Once
	promiseHash [sha256.Size]byte
}

// PromiseHash hashes the already derived sign bytes and signature once.
// A nil result leaves malformed inputs to the ordinary promise validator.
func (d *DecodedPayForFibre) PromiseHash() []byte {
	if d.Msg == nil || !d.PromiseKeyed || len(d.Msg.PaymentPromise.Signature) == 0 {
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

// FibreTx returns the tx with its synthesized system blob, built on first use.
func (d *DecodedPayForFibre) FibreTx() (*squaretx.FibreTx, error) {
	d.fibreTxOnce.Do(func() {
		systemBlob, err := d.Msg.SystemBlob()
		if err != nil {
			d.fibreTxErr = err
			return
		}
		d.fibreTx = &squaretx.FibreTx{Tx: d.Raw, SystemBlob: systemBlob}
	})
	return d.fibreTx, d.fibreTxErr
}

type decodedPayForFibreKey struct{}

// DecodedHolder carries the current transaction's decoded view through a
// sequential walk, which attaches one holder rather than a context value per
// transaction. It is not safe for concurrent use.
type DecodedHolder struct {
	decoded *DecodedPayForFibre
}

// Set points the holder at the decoded view of the transaction about to be
// processed. A nil d clears the one left by the previous transaction.
func (h *DecodedHolder) Set(d *DecodedPayForFibre) {
	h.decoded = d
}

// WithDecodedHolder attaches h to ctx once, for a whole walk.
func WithDecodedHolder(ctx sdk.Context, h *DecodedHolder) sdk.Context {
	if ctx.Context() == nil {
		ctx = ctx.WithContext(context.Background())
	}
	return ctx.WithValue(decodedPayForFibreKey{}, h)
}

// WithDecodedPayForFibre attaches d to ctx for a single transaction. Prefer
// `WithDecodedHolder` where a loop processes many transactions on one context.
func WithDecodedPayForFibre(ctx sdk.Context, d *DecodedPayForFibre) sdk.Context {
	return WithDecodedHolder(ctx, &DecodedHolder{decoded: d})
}

// DecodedPayForFibreFromContext returns the decoded view attached to ctx, and
// only if it was decoded from exactly ctx.TxBytes(), so a value left behind by
// another tx is never used.
func DecodedPayForFibreFromContext(ctx sdk.Context) *DecodedPayForFibre {
	if ctx.Context() == nil {
		return nil
	}
	h, _ := ctx.Value(decodedPayForFibreKey{}).(*DecodedHolder)
	if h == nil || h.decoded == nil || !bytes.Equal(h.decoded.Raw, ctx.TxBytes()) {
		return nil
	}
	return h.decoded
}
