package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

// TestPrepareProposalDropsTxThatPanicsInAnte checks that a tx whose ante
// handler panics is dropped from the proposal, and that the rest of the
// proposal is still built and accepted by ProcessProposal.
func TestPrepareProposalDropsTxThatPanicsInAnte(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(4)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)

	sendTxs := coretypes.Txs(testutil.SendTxsWithAccounts(
		t, testApp, enc.TxConfig, kr, 1000, accounts[0], accounts[1:], testutil.ChainID,
	)).ToSliceOfBytes()

	// Strip the fee from one tx: the ante handler dereferences the nil fee
	// and panics.
	var raw txtypes.TxRaw
	require.NoError(t, raw.Unmarshal(sendTxs[1]))
	var authInfo txtypes.AuthInfo
	require.NoError(t, authInfo.Unmarshal(raw.AuthInfoBytes))
	authInfo.Fee = nil
	authInfoBytes, err := authInfo.Marshal()
	require.NoError(t, err)
	raw.AuthInfoBytes = authInfoBytes
	panicTx, err := raw.Marshal()
	require.NoError(t, err)

	blockTime := time.Now()
	resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Txs:        [][]byte{sendTxs[0], panicTx, sendTxs[2]},
		MaxTxBytes: int64(appconsts.BlockMaxBytes),
		Height:     testApp.LastBlockHeight() + 1,
		Time:       blockTime,
	})
	require.NoError(t, err)
	want := [][]byte{sendTxs[0], sendTxs[2]}
	require.Equal(t, want, resp.Txs)

	expected := processProposalRequest(t, testApp, want)
	require.Equal(t, expected.SquareSize, resp.SquareSize)
	require.Equal(t, expected.DataRootHash, resp.DataRootHash)

	processResp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
		Height:       testApp.LastBlockHeight() + 1,
		Time:         blockTime,
		Txs:          resp.Txs,
		DataRootHash: resp.DataRootHash,
		SquareSize:   resp.SquareSize,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processResp.Status)
}
