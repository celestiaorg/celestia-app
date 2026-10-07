package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/test/util/fibrefactory"
	abci "github.com/cometbft/cometbft/abci/types"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

func TestProcessProposalParallelPayForFibre(t *testing.T) {
	const count = 32
	fixture := fibrefactory.NewFixture(t, count, 100)
	txs := fixture.Txs(t, 67)
	height := fixture.App.LastBlockHeight() + 1
	prepared, err := fixture.App.PrepareProposal(&abci.RequestPrepareProposal{
		Height: height,
		Time:   fixture.BlockTime,
		Txs:    txs,
	})
	require.NoError(t, err)
	require.Len(t, prepared.Txs, count)
	req := &abci.RequestProcessProposal{
		Height:       height,
		Time:         fixture.BlockTime,
		Txs:          prepared.Txs,
		SquareSize:   prepared.SquareSize,
		DataRootHash: prepared.DataRootHash,
	}

	validator := fixture.NewApp(t)
	process := func() {
		resp, err := validator.ProcessProposal(req)
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
	}
	process() // Cold: this app has not seen the transactions.
	for _, tx := range txs {
		resp, err := validator.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code, resp.Log)
	}
	process() // Warm: CheckTx admitted every transaction.

	// A valid validator certificate must not let a bad outer transaction
	// signature through, even when preverification runs in parallel.
	badValidator := fixture.NewApp(t)
	var raw cosmostx.TxRaw
	require.NoError(t, raw.Unmarshal(txs[0]))
	require.Len(t, raw.Signatures, 1)
	raw.Signatures[0] = append([]byte(nil), raw.Signatures[0]...)
	raw.Signatures[0][0] ^= 1
	badTx, err := raw.Marshal()
	require.NoError(t, err)
	badReq := processProposalRequest(t, badValidator, [][]byte{badTx})
	badReq.Time = fixture.BlockTime
	badResp, err := badValidator.ProcessProposal(badReq)
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, badResp.Status)
	checkResp, err := badValidator.CheckTx(&abci.RequestCheckTx{Tx: badTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.NotEqual(t, abci.CodeTypeOK, checkResp.Code)
	goodReq := processProposalRequest(t, badValidator, [][]byte{txs[0]})
	goodReq.Time = fixture.BlockTime
	goodResp, err := badValidator.ProcessProposal(goodReq)
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, goodResp.Status)
}
