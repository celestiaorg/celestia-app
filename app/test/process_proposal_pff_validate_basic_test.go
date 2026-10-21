package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	abci "github.com/cometbft/cometbft/abci/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

// TestProcessProposalRejectsPFFFailingValidateBasic: a PFF with an extra signature
// fails ValidateBasic, so ProcessProposal must reject it as FinalizeBlock does.
func TestProcessProposalRejectsPFFFailingValidateBasic(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	commitBlock(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), 1_000_000)

	valid := newSignedPayForFibreTx(t, signer, accounts[0], true)
	var raw txtypes.TxRaw
	require.NoError(t, raw.Unmarshal(valid))
	require.Len(t, raw.Signatures, 1)
	raw.Signatures = append(raw.Signatures, append([]byte(nil), raw.Signatures[0]...))
	tampered, err := raw.Marshal()
	require.NoError(t, err)

	// The tampered tx is invalid on its own: CheckTx rejects it.
	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: tampered, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.NotEqual(t, abci.CodeTypeOK, checkResp.Code)
	require.Contains(t, checkResp.Log, "wrong number of signers")

	txs := [][]byte{tampered}
	classified, err := fibretypes.ClassifyTxs(txs)
	require.NoError(t, err)
	dataSquare, err := square.Construct(classified, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	size, err := dataSquare.Size()
	require.NoError(t, err)

	resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
		Height:       testApp.LastBlockHeight() + 1,
		Time:         time.Now(),
		Txs:          txs,
		DataRootHash: calculateNewDataHash(t, txs),
		SquareSize:   uint64(size),
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status,
		"ProcessProposal accepted a PFF whose ValidateBasic fails; FinalizeBlock would fail it with its blob already in the square")
}
