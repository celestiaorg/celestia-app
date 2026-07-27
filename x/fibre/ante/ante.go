package ante

import (
	fibrekeeper "github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ sdk.AnteDecorator = FibreSignatureDecorator{}

// FibreSignatureDecorator verifies the validator signatures of MsgPayForFibre
// messages. The caller provides a cache for successful signature-verification
// results, keyed by raw transaction bytes, so the expensive verification runs
// once per transaction across CheckTx, PrepareProposal, ProcessProposal, and
// FinalizeBlock.
type FibreSignatureDecorator struct {
	keeper               *fibrekeeper.Keeper
	isVerificationCached func(tx []byte) bool
	cacheVerification    func(tx []byte)
}

func NewFibreSignatureDecorator(
	keeper *fibrekeeper.Keeper,
	isVerificationCached func(tx []byte) bool,
	cacheVerification func(tx []byte),
) FibreSignatureDecorator {
	return FibreSignatureDecorator{
		keeper:               keeper,
		isVerificationCached: isVerificationCached,
		cacheVerification:    cacheVerification,
	}
}

func (d FibreSignatureDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if simulate {
		return next(ctx, tx, simulate)
	}
	for _, msg := range tx.GetMsgs() {
		pff, ok := msg.(*fibretypes.MsgPayForFibre)
		if !ok {
			continue
		}
		rawTx := ctx.TxBytes()
		// Never use empty tx bytes as a cache key: it would alias every tx
		// whose bytes are missing from the context onto one entry.
		if len(rawTx) > 0 && d.isVerificationCached(rawTx) {
			continue
		}
		if err := d.keeper.ValidatePayForFibreSignatures(ctx, pff); err != nil {
			return ctx, err
		}
		if len(rawTx) > 0 {
			d.cacheVerification(rawTx)
		}
	}
	return next(ctx, tx, simulate)
}
