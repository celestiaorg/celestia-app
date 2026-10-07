package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/test/util/fibrefactory"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

func TestFinalizePayForFibreCacheHitMatchesColdState(t *testing.T) {
	app.NodeHome = t.TempDir()
	fixture := fibrefactory.NewFixture(t, 4, 3)
	txs := fixture.Txs(t, 2)
	finalize := func(warm bool) []byte {
		t.Helper()
		testApp := fixture.NewApp(t)
		if warm {
			for _, tx := range txs {
				checked, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
				require.NoError(t, err)
				require.Equal(t, abci.CodeTypeOK, checked.Code, checked.Log)
			}
		}
		finalized, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: testApp.LastBlockHeight() + 1,
			Time:   fixture.BlockTime,
			Hash:   testApp.LastCommitID().Hash,
			Txs:    txs,
		})
		require.NoError(t, err)
		require.Len(t, finalized.TxResults, len(txs))
		for _, result := range finalized.TxResults {
			require.Equal(t, abci.CodeTypeOK, result.Code, result.Log)
		}
		_, err = testApp.Commit()
		require.NoError(t, err)
		return testApp.LastCommitID().Hash
	}
	require.Equal(t, finalize(false), finalize(true))
}
