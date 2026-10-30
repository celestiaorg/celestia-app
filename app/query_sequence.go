package app

import (
	"context"
	"errors"
	"math"

	storetypes "cosmossdk.io/store/types"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/mempool"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// SetPendingSequencer injects the in-process mempool that QuerySequence reads
// pending sequences from. Without it QuerySequence falls back to checkState.
func (app *App) SetPendingSequencer(p mempool.PendingSequencer) {
	if p == nil {
		app.pendingSequencer.Store(nil)
		return
	}
	app.pendingSequencer.Store(&p)
}

// QuerySequence returns the signer's next sequence: the larger of its committed
// sequence and one past its highest sequence pending in the mempool. Neither
// depends on recheck progress, so the value does not drop while recheck
// rebuilds checkState after Commit. A pending tx that is evicted no longer
// counts, so the value falls back to the committed sequence.
//
// Without an in-process mempool (SetPendingSequencer not called) it reads
// checkState as before, which lags the true value during recheck.
func (app *App) QuerySequence(_ context.Context, req *abci.RequestQuerySequence) (*abci.ResponseQuerySequence, error) {
	p := app.pendingSequencer.Load()
	if p == nil {
		return app.querySequenceFromCheckState(req.Signer)
	}

	// Read the mempool first: a tx it drops after a commit is then already in
	// the committed state read below.
	pending, hasPending := (*p).HighestPendingSequence(req.Signer)

	ctx, ok := app.committedContext()
	if !ok {
		// No block committed yet.
		return app.querySequenceFromCheckState(req.Signer)
	}
	sequence, err := app.accountSequence(ctx, req.Signer)
	if err != nil {
		return nil, err
	}
	if hasPending && pending != math.MaxUint64 && pending+1 > sequence {
		sequence = pending + 1
	}
	return &abci.ResponseQuerySequence{Sequence: sequence}, nil
}

// storeCommittedContext caches a read-only context on the state Commit just
// wrote, so QuerySequence does not branch every store on each call.
func (app *App) storeCommittedContext() {
	ctx, err := app.CreateQueryContext(0, false)
	if err != nil {
		app.committedCtx.Store(nil)
		return
	}
	app.committedCtx.Store(&ctx)
}

// committedContext returns a context on the last committed state with its own
// gas meter, as the cached context is shared between callers.
func (app *App) committedContext() (sdk.Context, bool) {
	if ctx := app.committedCtx.Load(); ctx != nil {
		return ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()), true
	}
	// Before the first Commit since start.
	ctx, err := app.CreateQueryContext(0, false)
	return ctx, err == nil
}

// querySequenceFromCheckState returns the signer's sequence in checkState.
func (app *App) querySequenceFromCheckState(signer []byte) (*abci.ResponseQuerySequence, error) {
	app.checkStateMu.RLock()
	defer app.checkStateMu.RUnlock()

	checkTxCtx, ok := app.CheckState()
	if !ok {
		return &abci.ResponseQuerySequence{}, errors.New("checkState not set")
	}
	// Own gas meter: concurrent readers would otherwise share checkState's.
	sequence, err := app.accountSequence(checkTxCtx.WithGasMeter(storetypes.NewInfiniteGasMeter()), signer)
	if err != nil {
		return nil, err
	}
	return &abci.ResponseQuerySequence{Sequence: sequence}, nil
}

// accountSequence returns the signer's sequence in ctx, or 0 for an unknown account.
func (app *App) accountSequence(ctx sdk.Context, signer []byte) (uint64, error) {
	sequence, err := app.AccountKeeper.GetSequence(ctx, sdk.AccAddress(signer))
	if errors.Is(err, sdkerrors.ErrUnknownAddress) {
		return 0, nil
	}
	return sequence, err
}
