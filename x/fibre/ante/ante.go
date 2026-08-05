package ante

import (
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

// PffSigCache tracks txs whose PFF signatures were already checked.
type PffSigCache interface {
	IsCached(tx []byte) bool
	Cache(tx []byte)
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

	rawTx := ctx.TxBytes()
	// Empty tx bytes are not cacheable.
	if len(rawTx) > 0 && d.pffSigCache.IsCached(rawTx) {
		return next(ctx, tx, simulate)
	}

	verificationCtx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	if err := d.k.ValidatePayForFibreSignatures(verificationCtx, msg); err != nil {
		return ctx, err
	}
	if len(rawTx) > 0 {
		d.pffSigCache.Cache(rawTx)
	}
	return next(ctx, tx, simulate)
}

func payForFibreMessage(tx sdk.Tx) *fibretypes.MsgPayForFibre {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return nil
	}
	pff, _ := msgs[0].(*fibretypes.MsgPayForFibre)
	return pff
}
