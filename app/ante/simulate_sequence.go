package ante

import (
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// SimulateSequenceDecorator sets each signer's account sequence to the one in
// its signature when simulating, so the sequence check that follows cannot fail
// and gas estimation does not depend on recheck progress. It is a no-op outside
// simulation, and its reads and writes are not metered.
type SimulateSequenceDecorator struct {
	ak ante.AccountKeeper
}

func NewSimulateSequenceDecorator(ak ante.AccountKeeper) SimulateSequenceDecorator {
	return SimulateSequenceDecorator{ak: ak}
}

func (d SimulateSequenceDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !simulate {
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
		if acc == nil || acc.GetSequence() == sig.Sequence {
			continue
		}
		if err := acc.SetSequence(sig.Sequence); err != nil {
			return ctx, err
		}
		d.ak.SetAccount(unmetered, acc)
	}
	return next(ctx, tx, simulate)
}
