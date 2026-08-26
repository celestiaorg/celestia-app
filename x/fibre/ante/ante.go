package ante

import (
	"crypto/sha256"
	"time"

	storetypes "cosmossdk.io/store/types"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.AnteDecorator = FibreSignatureGasDecorator{}
	_ sdk.AnteDecorator = FibreSignatureVerificationDecorator{}
	_ sdk.AnteDecorator = FibreStatefulValidationDecorator{}
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

// FibreSignatureVerificationDecorator verifies uncached MsgPayForFibre
// signatures in CheckTx and ProcessProposal. FinalizeBlock always skips:
// a committed block already had its PFF signatures verified by honest
// validators in ProcessProposal, and the outcome must not depend on the
// node-local cache.
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
	if msg == nil || simulate || ctx.ExecMode() == sdk.ExecModeFinalize {
		return next(ctx, tx, simulate)
	}

	cacheKey, err := NewPffSigCacheKey(msg)
	if err != nil {
		return ctx, err
	}
	if d.pffSigCache.IsCached(cacheKey) {
		return next(ctx, tx, simulate)
	}

	if err = d.k.ValidatePayForFibreSignatures(withInfiniteGasMeter(ctx), msg); err != nil {
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

// FibrePromiseKeeper checks whether a payment promise can still settle.
type FibrePromiseKeeper interface {
	ValidatePaymentPromiseStateful(ctx sdk.Context, promise *fibretypes.PaymentPromise) (time.Time, error)
}

// FibreStatefulValidationDecorator keeps unsettleable MsgPayForFibre txs,
// such as replays of already-settled promises, out of the mempool. It runs
// only in CheckTx and recheck; PrepareProposal remains the authoritative
// settlement check.
type FibreStatefulValidationDecorator struct {
	k FibrePromiseKeeper
}

// NewFibreStatefulValidationDecorator returns a new FibreStatefulValidationDecorator.
func NewFibreStatefulValidationDecorator(k FibrePromiseKeeper) FibreStatefulValidationDecorator {
	return FibreStatefulValidationDecorator{k: k}
}

func (d FibreStatefulValidationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := PayForFibreMessage(tx)
	if msg == nil || simulate || !isMempoolExecMode(ctx) {
		return next(ctx, tx, simulate)
	}

	// Read-only check; infinite gas so a low tx gas limit cannot bypass it.
	if _, err := d.k.ValidatePaymentPromiseStateful(withInfiniteGasMeter(ctx), &msg.PaymentPromise); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

// isMempoolExecMode reports whether ctx executes under CheckTx or RecheckTx.
func isMempoolExecMode(ctx sdk.Context) bool {
	mode := ctx.ExecMode()
	return mode == sdk.ExecModeCheck || mode == sdk.ExecModeReCheck
}

// withInfiniteGasMeter returns ctx with an unlimited gas meter so verification
// is not constrained by the tx's gas limit.
func withInfiniteGasMeter(ctx sdk.Context) sdk.Context {
	return ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
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
