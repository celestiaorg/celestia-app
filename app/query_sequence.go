package app

import (
	"context"
	"errors"
	"fmt"
	"math"

	storetypes "cosmossdk.io/store/types"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/mempool"
	"github.com/cometbft/cometbft/node"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
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

type pendingSequencerSetter interface {
	SetPendingSequencer(mempool.PendingSequencer)
}

// WirePendingSequencer hands an in-process app (or a wrapper embedding *App)
// the node's mempool before the node starts; standalone start and testnode use it.
func WirePendingSequencer(a servertypes.Application, n *node.Node) error {
	s, ok := a.(pendingSequencerSetter)
	if !ok {
		return nil
	}
	p, ok := n.Mempool().(mempool.PendingSequencer)
	if !ok {
		return fmt.Errorf("mempool %T does not report pending sequences", n.Mempool())
	}
	s.SetPendingSequencer(p)
	return nil
}

// QuerySequence returns the signer's next sequence from the mempool and
// committed state (see nextSequence), so it does not lag during recheck.
func (app *App) QuerySequence(_ context.Context, req *abci.RequestQuerySequence) (*abci.ResponseQuerySequence, error) {
	sequence, ok, err := app.nextSequence(req.Signer)
	if err != nil {
		return nil, err
	}
	if !ok {
		return app.querySequenceFromCheckState(req.Signer)
	}
	return &abci.ResponseQuerySequence{Sequence: sequence}, nil
}

// nextSequence returns max(committed sequence, highest pending sequence + 1),
// or false without an in-process mempool or a Commit since start. Lock free.
func (app *App) nextSequence(signer []byte) (uint64, bool, error) {
	p := app.pendingSequencer.Load()
	if p == nil {
		return 0, false, nil
	}
	// Mempool first: a tx it drops after a Commit is then in the state read below.
	pending, hasPending := (*p).HighestPendingSequence(signer)
	committed := app.committedCtx.Load()
	if committed == nil {
		return 0, false, nil
	}
	sequence, err := app.accountSequence(committed.WithGasMeter(storetypes.NewInfiniteGasMeter()), signer)
	if err != nil {
		return 0, false, err
	}
	if hasPending && pending != math.MaxUint64 && pending+1 > sequence {
		sequence = pending + 1
	}
	return sequence, true, nil
}

// storeCommittedContext caches a read-only context on the state Commit just
// wrote; each reader gives it its own gas meter.
func (app *App) storeCommittedContext() {
	ctx, err := app.CreateQueryContext(0, false)
	if err != nil {
		app.committedCtx.Store(nil)
		return
	}
	app.committedCtx.Store(&ctx)
}

// querySequenceFromCheckState is the fallback when nextSequence has no answer
// (out-of-process node, no Commit yet); it lags the true value during recheck.
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
