package app_test

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
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

// TestRecheckTxsNilOnCheckTxError checks that a tx whose CheckTx errors gets a nil response,
// which core treats as it does a CheckTx error: the tx stays in the pool.
func TestRecheckTxsNilOnCheckTxError(t *testing.T) {
	a, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	bad := []byte{0xff, 0x01}
	_, err := a.CheckTx(&abci.RequestCheckTx{Tx: bad, Type: abci.CheckTxType_Recheck})
	require.Error(t, err)
	rsp, err := a.RecheckTxs(t.Context(), &abci.RequestRecheckTxs{Txs: [][]byte{bad}})
	require.NoError(t, err)
	require.Equal(t, []*abci.ResponseCheckTx{nil}, rsp.Responses)
}
