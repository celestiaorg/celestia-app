package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	"github.com/stretchr/testify/require"
)

// TestProposalRejectsMsgFailingValidateBasic covers a signed tx whose message
// fails ValidateBasic but whose ante handler passes. FinalizeBlock runs
// ValidateBasic before the ante handler, so such a tx would be included without
// paying fees. PrepareProposal must drop it and ProcessProposal must reject it.
func TestProposalRejectsMsgFailingValidateBasic(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	commitBlock(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	sender := testfactory.GetAddress(kr, accounts[0])

	// An empty source port fails MsgTransfer.ValidateBasic.
	msg := transfertypes.NewMsgTransfer(
		"", "channel-0",
		sdk.NewInt64Coin(appconsts.BondDenom, 1),
		sender.String(), sender.String(),
		clienttypes.NewHeight(0, 1000), 0, "",
	)
	require.Error(t, msg.ValidateBasic())
	invalid, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(200_000), user.SetFee(4_000))
	require.NoError(t, err)

	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: invalid, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.NotEqual(t, abci.CodeTypeOK, checkResp.Code)

	height := testApp.LastBlockHeight() + 1
	prepResp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Txs:    [][]byte{invalid},
		Height: height,
		Time:   time.Now(),
	})
	require.NoError(t, err)
	require.Empty(t, prepResp.Txs, "PrepareProposal kept a tx whose message fails ValidateBasic")

	txs := [][]byte{invalid}
	classified, err := fibretypes.ClassifyTxs(txs)
	require.NoError(t, err)
	dataSquare, err := square.Construct(classified, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	size, err := dataSquare.Size()
	require.NoError(t, err)

	procResp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
		Height:       height,
		Time:         time.Now(),
		Txs:          txs,
		DataRootHash: calculateNewDataHash(t, txs),
		SquareSize:   uint64(size),
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, procResp.Status,
		"ProcessProposal accepted a tx whose message fails ValidateBasic")

	// FinalizeBlock fails the tx before the ante handler: no fee is charged and
	// the sequence does not advance.
	balanceBefore := testApp.BankKeeper.GetBalance(testApp.NewContext(true), sender, appconsts.BondDenom)
	finResp := commitBlock(t, testApp, invalid)
	require.Len(t, finResp.TxResults, 1)
	require.NotEqual(t, abci.CodeTypeOK, finResp.TxResults[0].Code)
	balanceAfter := testApp.BankKeeper.GetBalance(testApp.NewContext(true), sender, appconsts.BondDenom)
	require.Equal(t, balanceBefore, balanceAfter)
	acc := testutil.DirectQueryAccount(testApp, sender)
	require.Equal(t, infos[0].Sequence, acc.GetSequence())
}
