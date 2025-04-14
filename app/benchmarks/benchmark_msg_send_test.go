//go:build benchmarks

package benchmarks_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

const blockTime = time.Duration(6 * time.Second)

func BenchmarkCheckTx_MsgSend_1(b *testing.B) {
	testApp, rawTxs := generateMsgSendTransactions(b, 1)

	finalizeBlockResp, err := testApp.FinalizeBlock(&types.RequestFinalizeBlock{
		Time:   testutil.GenesisTime.Add(blockTime),
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
	})
	require.NotNil(b, finalizeBlockResp)
	require.NoError(b, err)

	commitResp, err := testApp.Commit()
	require.NotNil(b, commitResp)
	require.NoError(b, err)

	checkTxReq := types.RequestCheckTx{
		Tx:   rawTxs[0],
		Type: types.CheckTxType_New,
	}

	b.ResetTimer()
	resp, err := testApp.CheckTx(&checkTxReq)
	require.NoError(b, err)
	b.StopTimer()
	require.Equal(b, uint32(0), resp.Code)
	require.Equal(b, "", resp.Codespace)
	b.ReportMetric(float64(resp.GasUsed), "gas_used")
}

func BenchmarkCheckTx_MsgSend_8MB(b *testing.B) {
	testApp, rawTxs := generateMsgSendTransactions(b, 31645)

	finalizeBlockResp, err := testApp.FinalizeBlock(&types.RequestFinalizeBlock{
		Time:   testutil.GenesisTime.Add(blockTime),
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
	})
	require.NotNil(b, finalizeBlockResp)
	require.NoError(b, err)

	commitResp, err := testApp.Commit()
	require.NotNil(b, commitResp)
	require.NoError(b, err)

	var totalGas int64
	b.ResetTimer()
	for _, tx := range rawTxs {
		checkTxRequest := types.RequestCheckTx{
			Tx:   tx,
			Type: types.CheckTxType_New,
		}
		b.StartTimer()
		resp, err := testApp.CheckTx(&checkTxRequest)
		require.NoError(b, err)
		b.StopTimer()
		require.Equal(b, uint32(0), resp.Code)
		require.Equal(b, "", resp.Codespace)
		totalGas += resp.GasUsed
	}

	b.StopTimer()
	b.ReportMetric(float64(totalGas), "total_gas_used")
}

func BenchmarkFinalizeBlock_MsgSend_1(b *testing.B) {
	testApp, rawTxs := generateMsgSendTransactions(b, 1)

	finalizeBlockReq := types.RequestFinalizeBlock{
		Time:   testutil.GenesisTime.Add(blockTime),
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
		Txs:    rawTxs,
	}

	b.ResetTimer()
	resp, err := testApp.FinalizeBlock(&finalizeBlockReq)
	require.NoError(b, err)
	b.StopTimer()
	require.Equal(b, uint32(0), resp.TxResults[0].Code)
	b.ReportMetric(float64(resp.TxResults[0].GasUsed), "gas_used")
}

func BenchmarkFinalizeBlock_MsgSend_8MB(b *testing.B) {
	b.ResetTimer()
	testApp, rawTxs := generateMsgSendTransactions(b, 31645)

	finalizeBlockReq := types.RequestFinalizeBlock{
		Time:   testutil.GenesisTime.Add(blockTime),
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
		Txs:    rawTxs,
	}

	b.StartTimer()
	resp, err := testApp.FinalizeBlock(&finalizeBlockReq)
	require.NoError(b, err)
	b.StopTimer()

	var totalGas int64
	for i := range rawTxs {
		require.Equal(b, uint32(0), resp.TxResults[i].Code)
		require.Equal(b, "", resp.TxResults[i].Codespace)
		totalGas += resp.TxResults[0].GasUsed
	}

	b.ReportMetric(float64(totalGas), "total_gas_used")
}

func BenchmarkPrepareProposal_MsgSend_1(b *testing.B) {
	testApp, rawTxs := generateMsgSendTransactions(b, 1)

	prepareProposalReq := types.RequestPrepareProposal{
		Txs:    rawTxs,
		Height: testApp.LastBlockHeight() + 1,
	}

	b.ResetTimer()
	resp, err := testApp.PrepareProposal(&prepareProposalReq)
	require.NoError(b, err)
	b.StopTimer()
	require.GreaterOrEqual(b, len(resp.Txs), 1)
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, resp.Txs)), "total_gas_used")
}

func BenchmarkPrepareProposal_MsgSend_8MB(b *testing.B) {
	// a full 8mb block equals to around 31645 msg send transactions.
	// using 31645 to let prepare proposal choose the maximum
	testApp, rawTxs := generateMsgSendTransactions(b, 31645)

	prepareProposalReq := types.RequestPrepareProposal{
		Txs:    rawTxs,
		Height: testApp.LastBlockHeight() + 1,
	}

	b.ResetTimer()
	resp, err := testApp.PrepareProposal(&prepareProposalReq)
	require.NoError(b, err)
	b.StopTimer()
	require.GreaterOrEqual(b, len(resp.Txs), 1)
	b.ReportMetric(float64(len(resp.Txs)), "number_of_transactions")
	b.ReportMetric(calculateBlockSizeInMb(resp.Txs), "block_size(mb)")
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, resp.Txs)), "total_gas_used")
}

func BenchmarkProcessProposal_MsgSend_1(b *testing.B) {
	testApp, rawTxs := generateMsgSendTransactions(b, 1)

	prepareProposalReq := types.RequestPrepareProposal{
		Txs:    rawTxs,
		Height: testApp.LastBlockHeight() + 1,
	}

	prepareProposalResp, err := testApp.PrepareProposal(&prepareProposalReq)
	require.NoError(b, err)
	require.Len(b, prepareProposalResp.Txs, 1)

	processProposalReq := types.RequestProcessProposal{
		Txs:          prepareProposalResp.Txs,
		Height:       testApp.LastBlockHeight() + 1,
		DataRootHash: prepareProposalResp.DataRootHash,
		SquareSize:   1,
	}

	b.ResetTimer()
	resp, err := testApp.ProcessProposal(&processProposalReq)
	require.NoError(b, err)
	b.StopTimer()
	require.Equal(b, types.ResponseProcessProposal_ACCEPT, resp.Status)

	b.ReportMetric(float64(calculateTotalGasUsed(testApp, prepareProposalResp.Txs)), "total_gas_used")
}

func BenchmarkProcessProposal_MsgSend_8MB(b *testing.B) {
	// a full 8mb block equals to around 31645 msg send transactions.
	// using 31645 to let prepare proposal choose the maximum
	testApp, rawTxs := generateMsgSendTransactions(b, 31645)

	prepareProposalReq := types.RequestPrepareProposal{
		Txs:    rawTxs,
		Height: testApp.LastBlockHeight() + 1,
	}

	prepareProposalResp, err := testApp.PrepareProposal(&prepareProposalReq)
	require.NoError(b, err)
	require.GreaterOrEqual(b, len(prepareProposalResp.Txs), 1)

	b.ReportMetric(float64(len(prepareProposalResp.Txs)), "number_of_transactions")
	b.ReportMetric(calculateBlockSizeInMb(prepareProposalResp.Txs), "block_size_(mb)")
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, prepareProposalResp.Txs)), "total_gas_used")

	processProposalReq := types.RequestProcessProposal{
		Txs:          prepareProposalResp.Txs,
		Height:       testApp.LastBlockHeight() + 1,
		DataRootHash: prepareProposalResp.DataRootHash,
		SquareSize:   128,
	}

	b.ResetTimer()
	resp, err := testApp.ProcessProposal(&processProposalReq)
	require.NoError(b, err)
	b.StopTimer()
	require.Equal(b, types.ResponseProcessProposal_ACCEPT, resp.Status)

	b.ReportMetric(float64(calculateTotalGasUsed(testApp, prepareProposalResp.Txs)), "total_gas_used")
}

func BenchmarkProcessProposal_MsgSend_8MB_Find_Half_Sec(b *testing.B) {
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

		prepareProposalReq := types.RequestPrepareProposal{
			Txs:    rawTxs[start:end],
			Height: testApp.LastBlockHeight() + 1,
		}

		prepareProposalResp, err := testApp.PrepareProposal(&prepareProposalReq)
		require.NoError(b, err)
		require.GreaterOrEqual(b, len(prepareProposalResp.Txs), 1)

		processProposalReq := types.RequestProcessProposal{
			Txs:          prepareProposalResp.Txs,
			Height:       testApp.LastBlockHeight() + 1,
			DataRootHash: prepareProposalResp.DataRootHash,
			SquareSize:   64,
		}

		startTime := time.Now()
		resp, err := testApp.ProcessProposal(&processProposalReq)
		require.NoError(b, err)
		endTime := time.Now()
		require.Equal(b, types.ResponseProcessProposal_ACCEPT, resp.Status)

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
	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
	require.NoError(b, err)
	rawTxs := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		msg := banktypes.NewMsgSend(
			addr,
			testnode.RandomAddress().(sdk.AccAddress),
			sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)),
		)
		rawTx, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(1000000), user.SetFee(10))
		require.NoError(b, err)
		rawTxs = append(rawTxs, rawTx)
		err = signer.IncrementSequence(account)
		require.NoError(b, err)
	}
	return testApp, rawTxs
}

// mebibyte the number of bytes in a mebibyte
const mebibyte = 1048576

// calculateBlockSizeInMb returns the block size in mb given a set
// of raw transactions.
func calculateBlockSizeInMb(txs [][]byte) float64 {
	numberOfBytes := 0
	for _, tx := range txs {
		numberOfBytes += len(tx)
	}
	mb := float64(numberOfBytes) / mebibyte
	return mb
}

// calculateTotalGasUsed simulates the provided transactions and returns the
// total gas used by all of them
func calculateTotalGasUsed(testApp *app.App, txs [][]byte) uint64 {
	var totalGas uint64
	for _, tx := range txs {
		gasInfo, _, _ := testApp.Simulate(tx)
		totalGas += gasInfo.GasUsed
	}
	return totalGas
}
