package benchmarks_test

import (
	"cosmossdk.io/log"
	"fmt"
	"github.com/celestiaorg/celestia-app/v7/app"
	"github.com/celestiaorg/celestia-app/v7/app/encoding"
	"github.com/celestiaorg/celestia-app/v7/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v7/test/util"
	"github.com/celestiaorg/celestia-app/v7/test/util/testfactory"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto"
	"github.com/stretchr/testify/require"
	"testing"
)

func init() {
	testutil.TestAppLogger = log.NewNopLogger()
}

func BenchmarkPrepareProposal_PFB_Multi(b *testing.B) {
	testCases := []struct {
		numberOfTransactions, blobSize int
	}{
		{numberOfTransactions: 106_000, blobSize: 300},
		{numberOfTransactions: 106_000, blobSize: 1000},
		{numberOfTransactions: 106_000, blobSize: 50_000},
		{numberOfTransactions: 106_000, blobSize: 500_000},
	}
	for _, testCase := range testCases {
		b.Run(fmt.Sprintf("%d transactions of %d bytes", testCase.numberOfTransactions, testCase.blobSize), func(b *testing.B) {
			benchmarkPrepareProposalPFB(b, testCase.numberOfTransactions, testCase.blobSize)
		})
	}
}

func benchmarkPrepareProposalPFB(b *testing.B, count, size int) {
	testApp, rawTxs := generatePayForBlobTransactions(b, count, size)

	prepareProposalReq := types.RequestPrepareProposal{
		Txs:    rawTxs,
		Height: testApp.LastBlockHeight() + 1,
	}

	b.ResetTimer()
	prepareProposalResp, err := testApp.PrepareProposal(&prepareProposalReq)
	require.NoError(b, err)
	b.StopTimer()
	require.GreaterOrEqual(b, len(prepareProposalResp.Txs), 1)
	b.ReportMetric(float64(b.Elapsed().Nanoseconds()), "prepare_proposal_time(ns)")
	b.ReportMetric(float64(len(prepareProposalResp.Txs)), "number_of_transactions")
	b.ReportMetric(float64(len(rawTxs[0])), "transactions_size(byte)")
	b.ReportMetric(calculateBlockSizeInMb(prepareProposalResp.Txs), "block_size(mb)")
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, rawTxs)), "total_gas_used")
}

func BenchmarkProcessProposal_PFB_Multi(b *testing.B) {
	testCases := []struct {
		numberOfTransactions, blobSize int
	}{
		{numberOfTransactions: 106_000, blobSize: 300},
		{numberOfTransactions: 106_000, blobSize: 500},
		{numberOfTransactions: 106_000, blobSize: 1000},
		{numberOfTransactions: 106_000, blobSize: 5000},
		{numberOfTransactions: 106_000, blobSize: 10_000},
		{numberOfTransactions: 106_000, blobSize: 50_000},
		{numberOfTransactions: 106_000, blobSize: 100_000},
		{numberOfTransactions: 106_000, blobSize: 200_000},
		{numberOfTransactions: 106_000, blobSize: 300_000},
		{numberOfTransactions: 106_000, blobSize: 400_000},
		{numberOfTransactions: 106_000, blobSize: 500_000},
	}
	for _, testCase := range testCases {
		b.Run(fmt.Sprintf("%d transactions of %d bytes", testCase.numberOfTransactions, testCase.blobSize), func(b *testing.B) {
			benchmarkProcessProposalPFB(b, testCase.numberOfTransactions, testCase.blobSize)
		})
	}
}

func benchmarkProcessProposalPFB(b *testing.B, count, size int) {
	testApp, rawTxs := generatePayForBlobTransactions(b, count, size)

	prepareProposalReq := types.RequestPrepareProposal{
		Txs:    rawTxs,
		Height: testApp.LastBlockHeight() + 1,
	}

	prepareProposalResp, err := testApp.PrepareProposal(&prepareProposalReq)
	require.NoError(b, err)
	require.GreaterOrEqual(b, len(prepareProposalResp.Txs), 1)

	processProposalReq := types.RequestProcessProposal{
		Txs:          prepareProposalResp.Txs,
		Height:       testApp.LastBlockHeight() + 1,
		DataRootHash: prepareProposalResp.DataRootHash,
		SquareSize:   prepareProposalResp.SquareSize,
	}

	b.ResetTimer()
	resp, err := testApp.ProcessProposal(&processProposalReq)
	require.NoError(b, err)
	b.StopTimer()
	require.Equal(b, types.ResponseProcessProposal_ACCEPT, resp.Status)

	b.ReportMetric(float64(b.Elapsed().Nanoseconds()), "process_proposal_time(ns)")
	b.ReportMetric(float64(len(prepareProposalResp.Txs)), "number_of_transactions")
	b.ReportMetric(float64(len(rawTxs[0])), "transactions_size(byte)")
	b.ReportMetric(calculateBlockSizeInMb(prepareProposalResp.Txs), "block_size(mb)")
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, rawTxs)), "total_gas_used")
}

// generatePayForBlobTransactions creates a test app then generates a number
// of valid PFB transactions in parallel across CPU cores.
func generatePayForBlobTransactions(b *testing.B, count, size int) (*app.App, [][]byte) {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 256, account)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)

	randomBytes := crypto.CRandBytes(size)
	blob, err := share.NewBlob(share.RandomNamespace(), randomBytes, 1, acc.GetAddress().Bytes())
	require.NoError(b, err)

	rawTxs := generateSignedTxsInParallel(b, kr, enc.TxConfig, testutil.ChainID, account, acc.GetAccountNumber(), acc.GetSequence(), count,
		func(signer *user.Signer, acctName string, _ int) ([]byte, error) {
			tx, _, err := signer.CreatePayForBlobs(acctName, []*share.Blob{blob}, user.SetGasLimit(2549760000), user.SetFee(10000))
			return tx, err
		},
	)
	return testApp, rawTxs
}
