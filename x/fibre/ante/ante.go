package ante

import (
	"crypto/sha256"
	"encoding/binary"

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
	if msg := payForFibreMessage(tx); msg != nil {
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
	msg := payForFibreMessage(tx)
	if msg == nil || simulate {
		return next(ctx, tx, simulate)
	}

	cacheKey, err := pffSigCacheKey(msg)
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

func pffSigCacheKey(msg *fibretypes.MsgPayForFibre) (PffSigCacheKey, error) {
	hasher := sha256.New()
	promise, err := msg.PaymentPromise.Marshal()
	if err != nil {
		return PffSigCacheKey{}, err
	}
	writeLengthPrefixed(hasher, promise)
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(msg.ValidatorSignatures)))
	_, _ = hasher.Write(count[:])
	for _, signature := range msg.ValidatorSignatures {
		writeLengthPrefixed(hasher, signature)
	}

	var key PffSigCacheKey
	copy(key[:], hasher.Sum(nil))
	return key, nil
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeLengthPrefixed(writer byteWriter, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write(value)
}

func payForFibreMessage(tx sdk.Tx) *fibretypes.MsgPayForFibre {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return nil
	}
	pff, _ := msgs[0].(*fibretypes.MsgPayForFibre)
	return pff
}
