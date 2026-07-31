package ante

import (
	storetypes "cosmossdk.io/store/types"
	fibrekeeper "github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.AnteDecorator = FibreSignatureGasDecorator{}
	_ sdk.AnteDecorator = FibreSignatureVerificationDecorator{}
)

// FibreSignatureGasDecorator charges deterministic MsgPayForFibre validation gas.
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

// FibreSignatureVerificationDecorator verifies uncached MsgPayForFibre validator signatures.
type FibreSignatureVerificationDecorator struct {
	verifySignatures     func(ctx sdk.Context, msg *fibretypes.MsgPayForFibre) error
	isVerificationCached func(tx []byte) bool
	cacheVerification    func(tx []byte)
}

func NewFibreSignatureVerificationDecorator(
	keeper *fibrekeeper.Keeper,
	isVerificationCached func(tx []byte) bool,
	cacheVerification func(tx []byte),
) FibreSignatureVerificationDecorator {
	return FibreSignatureVerificationDecorator{
		verifySignatures:     keeper.ValidatePayForFibreSignatures,
		isVerificationCached: isVerificationCached,
		cacheVerification:    cacheVerification,
	}
}

func (d FibreSignatureVerificationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := payForFibreMessage(tx)
	if msg == nil || simulate {
		return next(ctx, tx, simulate)
	}

	rawTx := ctx.TxBytes()
	// Empty tx bytes are not a valid cache key.
	if len(rawTx) > 0 && d.isVerificationCached(rawTx) {
		return next(ctx, tx, simulate)
	}

	verificationCtx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	if err := d.verifySignatures(verificationCtx, msg); err != nil {
		return ctx, err
	}
	if len(rawTx) > 0 {
		d.cacheVerification(rawTx)
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
