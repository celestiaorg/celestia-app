package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/ante"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
	coretypes "github.com/tendermint/tendermint/types"
)

// Create an ordered list of transactions,
// construct a square,
// then deconstruct to a list of transactions.
func reconstruct(testApp *app.App, height int64, blockTime time.Time, txs [][]byte, txConf client.TxConfig) [][]byte {
	// Setup
	sdkCtx := testApp.NewProposalContext(core.Header{
		ChainID: testutil.ChainID,
		Height:  height,
		Time:    blockTime,
	})
	handler := ante.NewAnteHandler(
		testApp.AccountKeeper,
		testApp.BankKeeper,
		testApp.BlobKeeper,
		testApp.FeeGrantKeeper,
		testApp.GetTxConfig().SignModeHandler(),
		ante.DefaultSigVerificationGasConsumer,
		testApp.IBCKeeper,
	)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	decoder := encCfg.TxConfig.TxDecoder()
	filteredTxs := app.FilterTxs(testApp.Logger(), sdkCtx, handler, txConf, txs)
	appVersion := testApp.GetBaseApp().AppVersion(sdkCtx)
	maxSquareSize := testApp.GovSquareSizeUpperBound(sdkCtx)

	// 1. Create ordered txs
	// Note this is called from PrepareProposal, the implementation under test, therefore this differential test cannot test square.Build.
	_, orderedTxs, err := square.Build(filteredTxs, appVersion, maxSquareSize)
	if err != nil {
		panic(err)
	}

	// 2. Construct a square
	dataSquare, err := square.Construct(orderedTxs, appVersion, maxSquareSize)
	if err != nil {
		panic(err)
	}

	// 3. Deconstruct to a list of txs
	txs1, err := square.Deconstruct(dataSquare, decoder)
	if err != nil {
		panic(err)
	}

	return txs1.ToSliceOfBytes()
}

// TestPrepareProposalConsistency produces blocks with random data using
// PrepareProposal and then tests those blocks by calling ProcessProposal. All
// blocks produced by PrepareProposal should be accepted by ProcessProposal. It
// doesn't use the standard go tools for fuzzing as those tools only support
// fuzzing limited types, instead we repeatedly create random blocks using
// various square sizes.
func TestPrepareProposalConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestPrepareProposalConsistency in short mode.")
	}
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := make([]string, 1100) // 1000 for creating blob txs, 100 for creating send txs
	for i := range accounts {
		accounts[i] = tmrand.Str(20)
	}
	maxShareCount := int64(appconsts.DefaultSquareSizeUpperBound * appconsts.DefaultSquareSizeUpperBound)

	type test struct {
		name                   string
		count, blobCount, size int
		iterations             int
	}
	tests := []test{
		// running these tests more than once in CI will sometimes timeout, so we
		// have to run them each once per square size. However, we can run these
		// more locally by increasing the iterations.
		{"many small single share single blob transactions", 1000, 1, 400, 1},
		{"one hundred normal sized single blob transactions", 100, 1, 400000, 1},
		{"many single share multi-blob transactions", 1000, 100, 400, 1},
		{"one hundred normal sized multi-blob transactions", 100, 4, 400000, 1},
	}

	type testSize struct {
		name             string
		maxBytes         int64
		govMaxSquareSize int
	}
	sizes := []testSize{
		{
			"default (should be 64 as of mainnet)",
			appconsts.DefaultMaxBytes,
			appconsts.DefaultGovMaxSquareSize,
		},
		{
			"max",
			maxShareCount * appconsts.ContinuationSparseShareContentSize,
			appconsts.DefaultSquareSizeUpperBound,
		},
		{
			"larger MaxBytes than SquareSize",
			maxShareCount * appconsts.ContinuationSparseShareContentSize,
			appconsts.DefaultGovMaxSquareSize,
		},
		{
			"smaller MaxBytes than SquareSize",
			32 * 32 * appconsts.ContinuationSparseShareContentSize,
			appconsts.DefaultGovMaxSquareSize,
		},
	}

	// run the above test case for each square size the specified number of
	// iterations
	for _, size := range sizes {
		// setup a new application with different MaxBytes consensus parameter
		// values.
		cparams := app.DefaultConsensusParams()
		cparams.Block.MaxBytes = size.maxBytes

		testApp, kr := testutil.SetupTestAppWithGenesisValSet(cparams, accounts...)

		sendTxCount := 100

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// repeat the test multiple times with random data each
				// iteration.
				for i := 0; i < tt.iterations; i++ {
					txs := testutil.RandBlobTxsWithAccounts(
						t,
						testApp,
						encConf.TxConfig,
						kr,
						tt.size,
						tt.count,
						true,
						testutil.ChainID,
						accounts[:tt.count],
						user.SetGasLimitAndFee(1_000_000_000, 0.1),
					)
					// create 100 send transactions
					sendTxs := testutil.SendTxsWithAccounts(
						t,
						testApp,
						encConf.TxConfig,
						kr,
						1000,
						accounts[0],
						accounts[len(accounts)-sendTxCount:],
						testutil.ChainID,
						user.SetGasLimitAndFee(1_000_000, 0.1),
					)
					txs = append(txs, sendTxs...)

					blockTime := time.Now()
					height := testApp.LastBlockHeight() + 1

					resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
						BlockData: &core.Data{
							Txs: coretypes.Txs(txs).ToSliceOfBytes(),
						},
						ChainId: testutil.ChainID,
						Time:    blockTime,
						Height:  height,
					})

					// check that the square size is smaller than or equal to
					// the specified size
					require.LessOrEqual(t, resp.BlockData.SquareSize, uint64(size.govMaxSquareSize))

					res := testApp.ProcessProposal(abci.RequestProcessProposal{
						BlockData: resp.BlockData,
						Header: core.Header{
							DataHash: resp.BlockData.Hash,
							ChainID:  testutil.ChainID,
							Version:  version.Consensus{App: appconsts.LatestVersion},
							Height:   height,
						},
					},
					)
					require.Equal(t, abci.ResponseProcessProposal_ACCEPT, res.Result)
					// At least all of the send transactions and one blob tx
					// should make it into the block. This should be expected to
					// change if PFB transactions are not separated and put into
					// their own namespace
					require.GreaterOrEqual(t, len(resp.BlockData.Txs), sendTxCount+1)

					// Differentially test the length of transactions returned by testApp.PrepareProposal.
					// Essentially, test by extracting the txs from a second data square made with square.Construct
					// then compare the length of this list and the list returned by testApp.PrepareProposal.
					// Discussion: https://github.com/celestiaorg/celestia-app/issues/2519
					reconstrucedTxs := reconstruct(testApp, height, blockTime, coretypes.Txs(txs).ToSliceOfBytes(), encConf.TxConfig)
					require.Equal(t, len(reconstrucedTxs), len(resp.BlockData.Txs))
				}
			})
		}
	}
}
