package app_test

import (
	"errors"
	"testing"

	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/stretchr/testify/require"
)

// TestRecheckTxsFallbackDiscardsBranches fails every parallel pass after its workers ran and
// its checks passed, and requires the sequential retry to match CheckTx(Type=Recheck) exactly:
// no fee, sequence or fast recheck record from the discarded branches may leak into it.
func TestRecheckTxsFallbackDiscardsBranches(t *testing.T) {
	e := newEqEnvWith(t, 300, 24, simtestutil.AppOptionsMap{server.FlagRecheckWorkers: 4})
	defer e.full.Close()
	defer e.fast.Close()
	e.enableBatch(true)
	failed := 0
	e.fast.SetRecheckFailParallel(func() error {
		failed++
		return errors.New("forced")
	})
	for round := range 10 {
		e.addNewTxs(40 + e.rng.Intn(40))
		txs := e.buildBlock(round)
		e.commit(txs)
		e.removeIncluded(txs)
		e.recheck()
		e.requireSameCheckState(round)
	}
	require.Positive(t, failed, "no parallel pass reached its merge")
	require.Zero(t, e.fast.ParallelRechecks())
}
