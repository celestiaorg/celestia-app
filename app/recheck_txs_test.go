package app_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/stretchr/testify/require"
)

// TestRecheckTxsMatchesSequential checks that RecheckTxs gives exactly the responses and
// check state of CheckTx(Type=Recheck) on each tx, over random pools and blocks.
func TestRecheckTxsMatchesSequential(t *testing.T) {
	seeds, rounds := 6, 25
	if n, err := strconv.Atoi(os.Getenv("RECHECK_EQ_SEEDS")); err == nil && n > 0 {
		seeds = n
	}
	if testing.Short() {
		seeds, rounds = 2, 10
	}
	for seed := range seeds {
		accounts := 16 + seed%33
		t.Run(fmt.Sprintf("seed=%d/accounts=%d", seed, accounts), func(t *testing.T) {
			e := newEqEnvWith(t, int64(200+seed), accounts, simtestutil.AppOptionsMap{})
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
