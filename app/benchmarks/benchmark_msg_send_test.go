//go:build bench_prepare_proposal

package benchmarks_test

import (
	"fmt"
	"github.com/tendermint/tendermint/libs/log"
	"testing"
	"time"

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
)

func BenchmarkCheckTx_MsgSend_1(b *testing.B) {
	testutil.TestAppLogger = log.NewNopLogger()
	testApp, rawTxs := generateMsgSendTransactions(b, 1)
	testApp.Commit()

	checkTxRequest := types.RequestCheckTx{
		Tx:   rawTxs[0],
		Type: types.CheckTxType_New,
	}

	b.ResetTimer()
	resp := testApp.CheckTx(checkTxRequest)
	b.StopTimer()
	require.Equal(b, uint32(0), resp.Code)
	require.Equal(b, "", resp.Codespace)
	b.ReportMetric(float64(resp.GasUsed), "gas_used")
}

func BenchmarkCheckTx_MsgSend_8MB(b *testing.B) {
	testutil.TestAppLogger = log.NewNopLogger()
	testApp, rawTxs := generateMsgSendTransactions(b, 31645)
	testApp.Commit()

	var totalGas int64
	b.ResetTimer()
	for _, tx := range rawTxs {
		checkTxRequest := types.RequestCheckTx{
			Tx:   tx,
			Type: types.CheckTxType_New,
		}
		b.StartTimer()
		resp := testApp.CheckTx(checkTxRequest)
		b.StopTimer()
		require.Equal(b, uint32(0), resp.Code)
		require.Equal(b, "", resp.Codespace)
		totalGas += resp.GasUsed
	}

	b.StopTimer()
	b.ReportMetric(float64(totalGas), "total_gas_used")
}

func BenchmarkDeliverTx_MsgSend_1(b *testing.B) {
	testutil.TestAppLogger = log.NewNopLogger()
	testApp, rawTxs := generateMsgSendTransactions(b, 1)

	deliverTxRequest := types.RequestDeliverTx{
		Tx: rawTxs[0],
	}

	b.ResetTimer()
	resp := testApp.DeliverTx(deliverTxRequest)
	b.StopTimer()
	require.Equal(b, uint32(0), resp.Code)
	require.Equal(b, "", resp.Codespace)
	b.ReportMetric(float64(resp.GasUsed), "gas_used")
}

func BenchmarkDeliverTx_MsgSend_8MB(b *testing.B) {
	testutil.TestAppLogger = log.NewNopLogger()
	testApp, rawTxs := generateMsgSendTransactions(b, 31645)

	var totalGas int64
	b.ResetTimer()
	for _, tx := range rawTxs {
		deliverTxRequest := types.RequestDeliverTx{
			Tx: tx,
		}
		b.StartTimer()
		resp := testApp.DeliverTx(deliverTxRequest)
		b.StopTimer()
		require.Equal(b, uint32(0), resp.Code)
		require.Equal(b, "", resp.Codespace)
		totalGas += resp.GasUsed
	}
	b.StopTimer()
	b.ReportMetric(float64(totalGas), "total_gas_used")
}

func BenchmarkPrepareProposal_MsgSend_1(b *testing.B) {
	testutil.TestAppLogger = log.NewNopLogger()
	testApp, rawTxs := generateMsgSendTransactions(b, 1)

	prepareProposalRequest := types.RequestPrepareProposal{
		BlockData: &tmproto.Data{
			Txs: rawTxs,
		},
		ChainId: testApp.GetChainID(),
		Height:  10,
	}

	b.ResetTimer()
	resp := testApp.PrepareProposal(prepareProposalRequest)
	b.StopTimer()
	require.GreaterOrEqual(b, len(resp.BlockData.Txs), 1)
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, resp.BlockData.Txs)), "total_gas_used")
}

func BenchmarkPrepareProposal_MsgSend_8MB(b *testing.B) {
	testutil.TestAppLogger = log.NewNopLogger()
	// a full 8mb block equals to around 31645 msg send transactions.
	// using 31645 to let prepare proposal choose the maximum
	testApp, rawTxs := generateMsgSendTransactions(b, 31645)

	blockData := &tmproto.Data{
		Txs: rawTxs,
	}
	prepareProposalRequest := types.RequestPrepareProposal{
		BlockData: blockData,
		ChainId:   testApp.GetChainID(),
		Height:    10,
	}

	b.ResetTimer()
	resp := testApp.PrepareProposal(prepareProposalRequest)
	b.StopTimer()
	require.GreaterOrEqual(b, len(resp.BlockData.Txs), 1)
	b.ReportMetric(float64(len(resp.BlockData.Txs)), "number_of_transactions")
	b.ReportMetric(calculateBlockSizeInMb(resp.BlockData.Txs), "block_size(mb)")
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, resp.BlockData.Txs)), "total_gas_used")
}

func BenchmarkProcessProposal_MsgSend_1(b *testing.B) {
	testutil.TestAppLogger = log.NewNopLogger()
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
	require.GreaterOrEqual(b, len(prepareProposalResponse.BlockData.Txs), 1)

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
	resp := testApp.ProcessProposal(processProposalRequest)
	b.StopTimer()
	require.Equal(b, types.ResponseProcessProposal_ACCEPT, resp.Result)

	b.ReportMetric(float64(calculateTotalGasUsed(testApp, prepareProposalResponse.BlockData.Txs)), "total_gas_used")
}

func BenchmarkProcessProposal_MsgSend_8MB(b *testing.B) {
	testutil.TestAppLogger = log.NewNopLogger()
	// a full 8mb block equals to around 31645 msg send transactions.
	// using 31645 to let prepare proposal choose the maximum
	testApp, rawTxs := generateMsgSendTransactions(b, 31645)

	blockData := &tmproto.Data{
		Txs: rawTxs,
	}
	prepareProposalRequest := types.RequestPrepareProposal{
		BlockData: blockData,
		ChainId:   testApp.GetChainID(),
		Height:    10,
	}
	prepareProposalResponse := testApp.PrepareProposal(prepareProposalRequest)
	require.GreaterOrEqual(b, len(prepareProposalResponse.BlockData.Txs), 1)

	b.ReportMetric(float64(len(prepareProposalResponse.BlockData.Txs)), "number_of_transactions")
	b.ReportMetric(calculateBlockSizeInMb(prepareProposalResponse.BlockData.Txs), "block_size_(mb)")
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, prepareProposalResponse.BlockData.Txs)), "total_gas_used")

	processProposalRequest := types.RequestProcessProposal{
		BlockData: prepareProposalResponse.BlockData,
		Header: tmproto.Header{
			Height:   10,
			DataHash: prepareProposalResponse.BlockData.Hash,
			ChainID:  testutil.ChainID,
			Version: version.Consensus{
				App: testApp.AppVersion(),
			},
		},
	}

	b.ResetTimer()
	resp := testApp.ProcessProposal(processProposalRequest)
	b.StopTimer()
	require.Equal(b, types.ResponseProcessProposal_ACCEPT, resp.Result)

	b.ReportMetric(float64(calculateTotalGasUsed(testApp, prepareProposalResponse.BlockData.Txs)), "total_gas_used")
}

func BenchmarkProcessProposal_MsgSend_8MB_Find_Half_Sec(b *testing.B) {
	testutil.TestAppLogger = log.NewNopLogger()
	targetTimeLowerBound := 0.499
	targetTimeUpperBound := 0.511
	numberOfTransaction := 5500
	testApp, rawTxs := generateMsgSendTransactions(b, numberOfTransaction)
	start := 0
	end := numberOfTransaction
	segment := end - start
	for {
		if segment == 1 {
			break
		}

		prepareProposalRequest := types.RequestPrepareProposal{
			BlockData: &tmproto.Data{
				Txs: rawTxs[start:end],
			},
			ChainId: testApp.GetChainID(),
			Height:  10,
		}
		prepareProposalResponse := testApp.PrepareProposal(prepareProposalRequest)
		require.GreaterOrEqual(b, len(prepareProposalResponse.BlockData.Txs), 1)

		processProposalRequest := types.RequestProcessProposal{
			BlockData: prepareProposalResponse.BlockData,
			Header: tmproto.Header{
				Height:   10,
				DataHash: prepareProposalResponse.BlockData.Hash,
				ChainID:  testutil.ChainID,
				Version: version.Consensus{
					App: testApp.AppVersion(),
				},
			},
		}

		startTime := time.Now()
		resp := testApp.ProcessProposal(processProposalRequest)
		endTime := time.Now()
		require.Equal(b, types.ResponseProcessProposal_ACCEPT, resp.Result)

		timeElapsed := float64(endTime.Sub(startTime).Nanoseconds()) / 1e9

		switch {
		case timeElapsed < targetTimeLowerBound:
			newEnd := end + segment/2
			if newEnd > len(rawTxs) {
				newEnd = len(rawTxs)
			}
			end = newEnd
			segment = end - start
			if segment <= 1 {
				break
			}
			continue
		case timeElapsed > targetTimeUpperBound:
			newEnd := end / 2
			if newEnd <= start {
				break
			}
			end = newEnd
			segment = end - start
			continue
		default:
			b.ReportMetric(timeElapsed, fmt.Sprintf("elapsedTime(s)_%d", end-start))
		}
		break
	}
}

// generateMsgSendTransactions creates a test app then generates a number
// of valid msg send transactions.
func generateMsgSendTransactions(b *testing.B, count int) (*app.App, [][]byte) {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 128, account)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)
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
		err = signer.IncrementSequence(account)
		require.NoError(b, err)
	}
	return testApp, rawTxs
}

// calculateBlockSizeInMb returns the block size in mb given a set
// of raw transactions.
func calculateBlockSizeInMb(txs [][]byte) float64 {
	numberOfBytes := 0
	for _, tx := range txs {
		numberOfBytes += len(tx)
	}
	mb := float64(numberOfBytes) / 1048576
	return mb
}

// calculateTotalGasUsed simulates the provided transactions and returns the
// total gas used by all of them
func calculateTotalGasUsed(testApp *app.App, txs [][]byte) uint64 {
	var totalGas uint64
	for _, tx := range txs {
		gasInfo, _, err := testApp.Simulate(tx)
		require.NoError(b, err)
		totalGas += gasInfo.GasUsed
	}
	return totalGas
}
