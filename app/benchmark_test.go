package app_test

import (
	"fmt"
	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
	"testing"
)

func BenchmarkCheckTx_MsgSend(b *testing.B) {
	testApp, rawTxs := generateMsgSendTransactions(b, 1)

	checkTxRequest := types.RequestCheckTx{
		Tx:   rawTxs[0],
		Type: types.CheckTxType_New,
	}

	b.ResetTimer()
	testApp.CheckTx(checkTxRequest)
}

func BenchmarkDeliverTx_MsgSend(b *testing.B) {
	testApp, rawTxs := generateMsgSendTransactions(b, 1)

	deliverTxRequest := types.RequestDeliverTx{
		Tx: rawTxs[0],
	}

	b.ResetTimer()
	testApp.DeliverTx(deliverTxRequest)
}

func BenchmarkPrepareProposal_MsgSend_1(b *testing.B) {
	testApp, rawTxs := generateMsgSendTransactions(b, 1)

	prepareProposalRequest := types.RequestPrepareProposal{
		BlockData: &tmproto.Data{
			Txs: rawTxs,
		},
		ChainId: testApp.GetChainID(),
		Height:  10,
	}

	b.ResetTimer()
	testApp.PrepareProposal(prepareProposalRequest)
}

func BenchmarkPrepareProposal_MsgSend_8MB(b *testing.B) {
	// a full 8mb block equals to around 39200 msg send transactions.
	// using 39300 to let prepare proposal choose the maximum
	testApp, rawTxs := generateMsgSendTransactions(b, 39300)

	blockData := &tmproto.Data{
		Txs: rawTxs,
	}
	prepareProposalRequest := types.RequestPrepareProposal{
		BlockData: blockData,
		ChainId:   testApp.GetChainID(),
		Height:    10,
	}

	b.ResetTimer()
	prepareProposalResponse := testApp.PrepareProposal(prepareProposalRequest)
	b.StopTimer()
	testApp.Logger().Info("block prepared", "number of transactions", len(prepareProposalResponse.BlockData.Txs), "block size (mb)~", calculateBlockSizeInMb(prepareProposalResponse.BlockData.Txs))
}

func BenchmarkProcessProposal_MsgSend_1(b *testing.B) {
	testApp, rawTxs := generateMsgSendTransactions(b, 1)

	blockData := &tmproto.Data{
		Txs: rawTxs,
	}
	prepareProposalRequest := types.RequestPrepareProposal{
		BlockData: blockData,
		ChainId:   testApp.GetChainID(),
		Height:    10,
	}
	prepareProposalResponse := testApp.PrepareProposal(prepareProposalRequest)

	processProposalRequest := types.RequestProcessProposal{
		BlockData: prepareProposalResponse.BlockData,
		Header: tmproto.Header{
			Height:   1,
			DataHash: prepareProposalResponse.BlockData.Hash,
			ChainID:  testutil.ChainID,
			Version: version.Consensus{
				App: testApp.AppVersion(),
			},
		},
	}

	b.ResetTimer()
	testApp.ProcessProposal(processProposalRequest)
}

func BenchmarkProcessProposal_MsgSend_8MB(b *testing.B) {
	// a full 8mb block equals to around 39200 msg send transactions.
	// using 39300 to let prepare proposal choose the maximum
	testApp, rawTxs := generateMsgSendTransactions(b, 39300)

	blockData := &tmproto.Data{
		Txs: rawTxs,
	}
	prepareProposalRequest := types.RequestPrepareProposal{
		BlockData: blockData,
		ChainId:   testApp.GetChainID(),
		Height:    10,
	}
	prepareProposalResponse := testApp.PrepareProposal(prepareProposalRequest)

	testApp.Logger().Info("block prepared", "number of transactions", len(prepareProposalResponse.BlockData.Txs), "block size (mb)~", calculateBlockSizeInMb(prepareProposalResponse.BlockData.Txs))

	processProposalRequest := types.RequestProcessProposal{
		BlockData: prepareProposalResponse.BlockData,
		Header: tmproto.Header{
			Height:   1,
			DataHash: prepareProposalResponse.BlockData.Hash,
			ChainID:  testutil.ChainID,
			Version: version.Consensus{
				App: testApp.AppVersion(),
			},
		},
	}

	b.ResetTimer()
	testApp.ProcessProposal(processProposalRequest)
}

// generateMsgSendTransactions creates a test app then generates a number
// of valid msg send transactions.
func generateMsgSendTransactions(b *testing.B, count int) (*app.App, [][]byte) {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), account)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)
	accountSequence := acc.GetSequence()
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
	require.NoError(b, err)
	rawTxs := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		msg := banktypes.NewMsgSend(
			addr,
			testnode.RandomAddress().(sdk.AccAddress),
			sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)),
		)
		rawTx, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(1000000), user.SetFee(10))
		require.NoError(b, err)
		rawTxs = append(rawTxs, rawTx)
		accountSequence++
		err = signer.SetSequence(account, accountSequence)
		require.NoError(b, err)
	}
	return testApp, rawTxs
}

// calculateBlockSizeInMb returns the block size in mb given a set
// of raw transactions.
func calculateBlockSizeInMb(txs [][]byte) string {
	numberOfBytes := 0
	for _, tx := range txs {
		numberOfBytes += len(tx)
	}
	mb := float64(numberOfBytes) / 1048576
	return fmt.Sprintf("%.3f", mb)
}
