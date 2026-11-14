package ante

import (
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// PendingNextSequenceFn returns one past the signer's highest pending sequence,
// or false if it has none. It must not take app locks: Simulate holds them.
type PendingNextSequenceFn func(signer []byte) (uint64, bool)

// SimulateSequenceDecorator raises a lagging checkState sequence, unmetered, past
// the signer's pending txs (committed state never exceeds it here), so during
// recheck only next passes and any other sequence fails reporting next.
type SimulateSequenceDecorator struct {
	ak   ante.AccountKeeper
	next PendingNextSequenceFn
}

func NewSimulateSequenceDecorator(ak ante.AccountKeeper, next PendingNextSequenceFn) SimulateSequenceDecorator {
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
	signers, err := sigTx.GetSigners()
	if err != nil {
		return next(ctx, tx, simulate)
	}

	unmetered := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	for _, signer := range signers {
		acc := d.ak.GetAccount(unmetered, signer)
		if acc == nil {
			continue
		}
		want, ok := d.next(signer)
		if !ok || want <= acc.GetSequence() {
			continue
		}
		if err := acc.SetSequence(want); err != nil {
			return ctx, err
		}
		d.ak.SetAccount(unmetered, acc)
	}
	return next(ctx, tx, simulate)
}
