package app_test

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/stretchr/testify/require"
)

// TestRecheckTxsMatchesSequential checks that a parallel RecheckTxs gives exactly the
// responses and check state of CheckTx(Type=Recheck) on each tx, over random pools and blocks.
func TestRecheckTxsMatchesSequential(t *testing.T) {
	seeds, rounds := 6, 25
	if n, err := strconv.Atoi(os.Getenv("RECHECK_EQ_SEEDS")); err == nil && n > 0 {
		seeds = n
	}
	if testing.Short() {
		seeds, rounds = 2, 10
	}
	for seed := range seeds {
		rng := rand.New(rand.NewSource(int64(seed)))
		workers := []int{2, 3, 4, 8, 16}[rng.Intn(5)]
		accounts := 16 + rng.Intn(33)
		t.Run(fmt.Sprintf("seed=%d/workers=%d/accounts=%d", seed, workers, accounts), func(t *testing.T) {
			e := newEqEnvWith(t, int64(200+seed), accounts, simtestutil.AppOptionsMap{server.FlagRecheckWorkers: workers})
			defer e.full.Close()
			defer e.fast.Close()
			e.enableBatch(true)
			for round := range rounds {
				e.addNewTxs(20 + e.rng.Intn(60))
				txs := e.buildBlock(round)
				e.commit(txs)
				e.removeIncluded(txs)
				e.recheck()
				e.requireSameCheckState(round)
			}
			t.Logf("parallel batches %d/%d", e.fast.ParallelRechecks(), rounds)
			require.Positive(t, e.fast.ParallelRechecks(), "no batch recheck ran in parallel")
		})
	}
}

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
