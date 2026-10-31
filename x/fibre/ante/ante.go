package ante

import (
	"time"

	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
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
	if msg := fibretypes.PayForFibreMsg(tx); msg != nil {
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
// node-local cache. Recheck also skips: the signatures cover immutable
// promise bytes, so nothing recheck observes can invalidate a result CheckTx
// already reached.
type FibreSignatureVerificationDecorator struct {
	k           FibreKeeper
	pffSigCache PffSigCache
}

// FibreKeeper verifies PFF signatures.
type FibreKeeper interface {
	ValidatePayForFibreSignatures(ctx sdk.Context, msg *fibretypes.MsgPayForFibre) error
	ValidateValidatorSetAvailable(ctx sdk.Context, height int64) error
	// PreverifyPayForFibreMsg verifies msg's signatures across every CPU and
	// records them if they all pass. It reports nothing: the caller reads the
	// cache, and ValidatePayForFibreSignatures still decides.
	PreverifyPayForFibreMsg(ctx sdk.Context, msg *fibretypes.MsgPayForFibre, certKey sigcache.Key)
}

// PffSigCache tracks PFF certificates whose signatures were already checked.
type PffSigCache interface {
	Has(key sigcache.Key) bool
	Add(key sigcache.Key)
}

// NewFibreSigVerificationDecorator returns a PFF signature verification decorator.
func NewFibreSigVerificationDecorator(
	k FibreKeeper,
	pffSigCache PffSigCache,
) FibreSignatureVerificationDecorator {
	return FibreSignatureVerificationDecorator{k: k, pffSigCache: pffSigCache}
}

func (d FibreSignatureVerificationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := fibretypes.PayForFibreMsg(tx)
	if msg == nil || simulate || skipsSignatureVerification(ctx.ExecMode()) {
		return next(ctx, tx, simulate)
	}

	// Reuse the key this phase already derived for this tx, if any.
	cacheKey, ok := fibretypes.CertKeyFromContext(ctx)
	if !ok {
		var err error
		cacheKey, err = msg.SigCacheKey()
		if err != nil {
			return ctx, err
		}
	}
	if d.pffSigCache.Has(cacheKey) {
		// The cache covers the signatures, not whether the validator set they
		// were checked against is still stored.
		if err := d.k.ValidateValidatorSetAvailable(withInfiniteGasMeter(ctx), msg.PaymentPromise.Height); err != nil {
			return ctx, err
		}
		return next(ctx, tx, simulate)
	}

	// CheckTx reaches this once gas, fees, authentication and fibre admission
	// have passed, so the pass is spent on an otherwise admissible tx. After
	// the lookup, never before: warming a cached entry is pure loss. The other
	// phases pre-verify their whole block, and recheck has nothing to warm.
	if ctx.IsCheckTx() && !ctx.IsReCheckTx() {
		d.k.PreverifyPayForFibreMsg(withInfiniteGasMeter(ctx), msg, cacheKey)
		if d.pffSigCache.Has(cacheKey) {
			return next(ctx, tx, simulate)
		}
		// A failed tx signature already rejects the tx, whatever the fallback finds.
		if fibretypes.TxSigFailed(ctx) {
			return ctx, errTxSigFailed
		}
	}

	if err := d.k.ValidatePayForFibreSignatures(withInfiniteGasMeter(ctx), msg); err != nil {
		return ctx, err
	}
	d.pffSigCache.Add(cacheKey)
	return next(ctx, tx, simulate)
}

// errTxSigFailed is replaced by the tx signature's own error before CheckTx returns.
var errTxSigFailed = errorsmod.Wrap(sdkerrors.ErrUnauthorized, "tx signature failed")

// skipsSignatureVerification reports whether mode never verifies PFF validator
// signatures. See FibreSignatureVerificationDecorator for why.
func skipsSignatureVerification(mode sdk.ExecMode) bool {
	return mode == sdk.ExecModeFinalize || mode == sdk.ExecModeReCheck
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
	msg := fibretypes.PayForFibreMsg(tx)
	if msg == nil || simulate || !ctx.IsCheckTx() {
		return next(ctx, tx, simulate)
	}

	// Read-only check; infinite gas so a low tx gas limit cannot bypass it.
	if _, err := d.k.ValidatePaymentPromiseStateful(withInfiniteGasMeter(ctx), &msg.PaymentPromise); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

// withInfiniteGasMeter returns ctx with an unlimited gas meter so verification
// is not constrained by the tx's gas limit.
func withInfiniteGasMeter(ctx sdk.Context) sdk.Context {
	return ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
}
