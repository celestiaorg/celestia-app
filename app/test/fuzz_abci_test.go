package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

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
		// running these tests more than once in CI will sometimes timout, so we
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

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// repeat the test multiple times with random data each
				// iteration.
				for i := 0; i < tt.iterations; i++ {
					txs := testutil.RandBlobTxsWithAccounts(
						t,
						testApp,
						encConf.TxConfig.TxEncoder(),
						kr,
						tt.size,
						tt.count,
						true,
						"",
						accounts[:tt.count],
					)
					// create 100 send transactions
					sendTxs := testutil.SendTxsWithAccounts(
						t,
						testApp,
						encConf.TxConfig.TxEncoder(),
						kr,
						1000,
						accounts[0],
						accounts[len(accounts)-100:],
						"",
					)
					txs = append(txs, sendTxs...)
					resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
						BlockData: &core.Data{
							Txs: coretypes.Txs(txs).ToSliceOfBytes(),
						},
						ChainId: testutil.ChainID,
					})

					// check that the square size is smaller than or equal to
					// the specified size
					require.LessOrEqual(t, resp.BlockData.SquareSize, uint64(size.govMaxSquareSize))

					res := testApp.ProcessProposal(abci.RequestProcessProposal{
						BlockData: resp.BlockData,
						Header: core.Header{
							DataHash: resp.BlockData.Hash,
						},
					})
					require.Equal(t, abci.ResponseProcessProposal_ACCEPT, res.Result)
				}
			})
		}
	}
}
