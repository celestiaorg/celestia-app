package ante

import (
	"crypto/sha256"

	storetypes "cosmossdk.io/store/types"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.AnteDecorator = FibreSignatureGasDecorator{}
	_ sdk.AnteDecorator = FibreSignatureVerificationDecorator{}
)

// FibreSignatureGasDecorator charges gas for MsgPayForFibre signature checks.
type FibreSignatureGasDecorator struct{}

func NewFibreSignatureGasDecorator() FibreSignatureGasDecorator {
	return FibreSignatureGasDecorator{}
}

func (FibreSignatureGasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if msg := PayForFibreMessage(tx); msg != nil {
		consumeDeterministicFibreSignatureGas(ctx, msg)
	}
	return next(ctx, tx, simulate)
}

func consumeDeterministicFibreSignatureGas(ctx sdk.Context, msg *fibretypes.MsgPayForFibre) {
	ctx.GasMeter().ConsumeGas(
		fibretypes.EstimateGasForPayForFibreSignatureVerification(uint64(len(msg.ValidatorSignatures))),
		"ante verify: pay for fibre validator signatures",
	)
}

// FibreSignatureVerificationDecorator verifies uncached MsgPayForFibre signatures.
type FibreSignatureVerificationDecorator struct {
	k           FibreKeeper
	pffSigCache PffSigCache
}

// FibreKeeper verifies PFF signatures.
type FibreKeeper interface {
	ValidatePayForFibreSignatures(ctx sdk.Context, msg *fibretypes.MsgPayForFibre) error
}

// PffSigCacheKey identifies all inputs to PFF signature verification.
type PffSigCacheKey [sha256.Size]byte

// PffSigCache tracks PFF certificates whose signatures were already checked.
type PffSigCache interface {
	IsCached(key PffSigCacheKey) bool
	Cache(key PffSigCacheKey)
}

// NewFibreSigVerificationDecorator returns a PFF signature verification decorator.
func NewFibreSigVerificationDecorator(
	k FibreKeeper,
	pffSigCache PffSigCache,
) FibreSignatureVerificationDecorator {
	return FibreSignatureVerificationDecorator{k: k, pffSigCache: pffSigCache}
}

func (d FibreSignatureVerificationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := PayForFibreMessage(tx)
	if msg == nil || simulate {
		return next(ctx, tx, simulate)
	}

	cacheKey, err := NewPffSigCacheKey(msg)
	if err != nil {
		return ctx, err
	}
	if d.pffSigCache.IsCached(cacheKey) {
		return next(ctx, tx, simulate)
	}

	verificationCtx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	if err = d.k.ValidatePayForFibreSignatures(verificationCtx, msg); err != nil {
		return ctx, err
	}
	d.pffSigCache.Cache(cacheKey)
	return next(ctx, tx, simulate)
}

// NewPffSigCacheKey derives the cache key for msg's certificate: the message
// without its signer, so the key covers exactly the inputs to signature
// verification. Proto encoding length-prefixes every field, so distinct
// certificates cannot collide.
func NewPffSigCacheKey(msg *fibretypes.MsgPayForFibre) (PffSigCacheKey, error) {
	certificate := fibretypes.MsgPayForFibre{
		PaymentPromise:      msg.PaymentPromise,
		ValidatorSignatures: msg.ValidatorSignatures,
	}
	bz, err := certificate.Marshal()
	if err != nil {
		return PffSigCacheKey{}, err
	}
	return sha256.Sum256(bz), nil
}

// PayForFibreMessage returns tx's MsgPayForFibre, or nil if tx is not a
// single-message PFF transaction.
func PayForFibreMessage(tx sdk.Tx) *fibretypes.MsgPayForFibre {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return nil
	}
	pff, _ := msgs[0].(*fibretypes.MsgPayForFibre)
	return pff
}
