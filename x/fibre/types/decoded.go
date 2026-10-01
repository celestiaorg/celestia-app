package types

import (
	"bytes"
	"context"

	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DecodedPayForFibre is a MsgPayForFibre tx decoded once per ABCI phase, with
// the values later steps of that phase would otherwise derive again from the
// same bytes. Everything in it is a pure function of Raw.
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

	fibreTx *squaretx.FibreTx
}

// FibreTx returns the tx with its synthesized system blob, built on first use.
func (d *DecodedPayForFibre) FibreTx() (*squaretx.FibreTx, error) {
	if d.fibreTx != nil {
		return d.fibreTx, nil
	}
	systemBlob, err := d.Msg.SystemBlob()
	if err != nil {
		return nil, err
	}
	d.fibreTx = &squaretx.FibreTx{Tx: d.Raw, SystemBlob: systemBlob}
	return d.fibreTx, nil
}

type decodedPayForFibreKey struct{}

// WithDecodedPayForFibre attaches d to ctx for the tx being processed. A nil d
// clears a value left by a previous tx.
func WithDecodedPayForFibre(ctx sdk.Context, d *DecodedPayForFibre) sdk.Context {
	if ctx.Context() == nil {
		ctx = ctx.WithContext(context.Background())
	}
	return ctx.WithValue(decodedPayForFibreKey{}, d)
}

// DecodedPayForFibreFromContext returns the decoded view attached to ctx, and
// only if it was decoded from exactly ctx.TxBytes(), so a value left behind by
// another tx is never used.
func DecodedPayForFibreFromContext(ctx sdk.Context) *DecodedPayForFibre {
	if ctx.Context() == nil {
		return nil
	}
	d, _ := ctx.Value(decodedPayForFibreKey{}).(*DecodedPayForFibre)
	if d == nil || !bytes.Equal(d.Raw, ctx.TxBytes()) {
		return nil
	}
	return d
}
