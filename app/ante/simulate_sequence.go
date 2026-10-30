package ante

import (
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// NextSequenceFn returns a signer's next sequence including its pending txs,
// or false if unknown. It must not take app locks: Simulate holds them.
type NextSequenceFn func(signer []byte) (uint64, bool)

// SimulateSequenceDecorator lifts a lagging checkState sequence, unmetered, up
// to the tx's when the tx uses the signer's next sequence, so gas estimation
// does not depend on recheck progress. Any other sequence still fails as before.
type SimulateSequenceDecorator struct {
	ak   ante.AccountKeeper
	next NextSequenceFn
}

func NewSimulateSequenceDecorator(ak ante.AccountKeeper, next NextSequenceFn) SimulateSequenceDecorator {
	return SimulateSequenceDecorator{ak: ak, next: next}
}

func (d SimulateSequenceDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !simulate || d.next == nil {
		return next(ctx, tx, simulate)
	}
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return next(ctx, tx, simulate)
	}
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return next(ctx, tx, simulate)
	}
	signers, err := sigTx.GetSigners()
	if err != nil || len(signers) != len(sigs) {
		return next(ctx, tx, simulate)
	}

	unmetered := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	for i, sig := range sigs {
		acc := d.ak.GetAccount(unmetered, signers[i])
		if acc == nil || sig.Sequence <= acc.GetSequence() {
			continue
		}
		if want, ok := d.next(signers[i]); !ok || sig.Sequence != want {
			continue
		}
		if err := acc.SetSequence(sig.Sequence); err != nil {
			return ctx, err
		}
		d.ak.SetAccount(unmetered, acc)
	}
	return next(ctx, tx, simulate)
}
