package app_test

import (
	"testing"
	"time"
	"math/rand"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v6/test/util"
	"github.com/celestiaorg/celestia-app/v6/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/go-square/v3/share"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
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
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := make([]string, 1100) // 1000 for creating blob txs, 100 for creating send txs
	for i := range accounts {
		accounts[i] = random.Str(20)
	}
	maxShareCount := int64(appconsts.SquareSizeUpperBound * appconsts.SquareSizeUpperBound)

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
			"default",
			appconsts.DefaultMaxBytes,
			appconsts.DefaultGovMaxSquareSize,
		},
		{
			"max",
			maxShareCount * share.ContinuationSparseShareContentSize,
			appconsts.SquareSizeUpperBound,
		},
		{
			"larger MaxBytes than SquareSize",
			maxShareCount * share.ContinuationSparseShareContentSize,
			appconsts.DefaultGovMaxSquareSize,
		},
		{
			"smaller MaxBytes than SquareSize",
			32 * 32 * share.ContinuationSparseShareContentSize,
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
						enc.TxConfig,
						kr,
						tt.size,
						tt.count,
						true,
						testutil.ChainID,
						accounts[:tt.count],
						user.SetGasLimitAndGasPrice(1_000_000_000, 0.1),
					)
					
					// create 100 send transactions
					sendTxs := testutil.SendTxsWithAccounts(
						t,
						testApp,
						enc.TxConfig,
						kr,
						1000,
						accounts[0],
						accounts[len(accounts)-sendTxCount:],
						testutil.ChainID,
						user.SetGasLimitAndGasPrice(1_000_000, 0.1),
					)
					txs = append(txs, sendTxs...)

					blockTime := time.Now()
					height := testApp.LastBlockHeight() + 1

					resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
						Txs:    coretypes.Txs(txs).ToSliceOfBytes(),
						Time:   blockTime,
						Height: height,
					})
					require.NoError(t, err)

					// check that the square size is smaller than or equal to
					// the specified size
					require.LessOrEqual(t, resp.SquareSize, uint64(size.govMaxSquareSize))

					res, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
						Height:       height,
						DataRootHash: resp.DataRootHash,
						SquareSize:   resp.SquareSize,
						Txs:          resp.Txs,
					},
					)
					require.NoError(t, err)

					require.Equal(t, abci.ResponseProcessProposal_ACCEPT, res.Status)
					// At least all of the send transactions and one blob tx
					// should make it into the block. This should be expected to
					// change if PFB transactions are not separated and put into
					// their own namespace
					require.GreaterOrEqual(t, len(resp.Txs), sendTxCount+1)
				}
			})
		}
	}
}


// TestPrepareProposalInclusion produces blocks with random data using
// PrepareProposal and then tests those blocks by calling ProcessProposal.
// It ensure the inclusion rate of blob in a block is constant
// we use both randomblobs and constant size PFB transaction to test the inclusion rate
// not all randomblobs produced will get included but constant size PFB transactions will get included
func TestPrepareProposalInclusion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestPrepareProposalInclusion in short mode.")
	}
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := make([]string, 1100) // 1000 for creating blob txs, 100 for creating send txs
	for i := range accounts {
		accounts[i] = random.Str(20)
	}
	maxShareCount := int64(appconsts.SquareSizeUpperBound * appconsts.SquareSizeUpperBound)

	type test struct {
		name                   string
		count, blobCount, minsize, maxsize int 
		iterations             int
		rate float64
	}
	tests := []test{
		// running these tests more than once in CI will sometimes timeout, so we
		// have to run them each once per square size. However, we can run these
		// more locally by increasing the iterations.
		{"many small single share single blob transactions", 500, 1, 1,400, 1, 0.04},
		{"one hundred normal sized single blob transactions", 100, 1, 10000,400000, 1, 0.1},

		// the range of those test are to big so with random with have inconsistencty
		{"many single share multi-blob transactions", 1000, 1000, 1,400, 1, 0.02}, // rates with random true are lower
		{"one hundred normal sized multi-blob transactions", 100, 10,1000, 400000, 1, 0.1}, // rates with random true are lower
	}

	type testSize struct {
		name             string
		maxBytes         int64
		govMaxSquareSize int
	}
	sizes := []testSize{
		{
			"default",
			appconsts.DefaultMaxBytes,
			appconsts.DefaultGovMaxSquareSize,
		},
		{
			"max",
			maxShareCount * share.ContinuationSparseShareContentSize,
			appconsts.SquareSizeUpperBound,
		},
		{
			"larger MaxBytes than SquareSize",
			maxShareCount * share.ContinuationSparseShareContentSize,
			appconsts.DefaultGovMaxSquareSize,
		},
		{
			"smaller MaxBytes than SquareSize",
			32 * 32 * share.ContinuationSparseShareContentSize,
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
				// repeat and generate PFB each time
				for i := 0; i < tt.iterations; i++ {
					// generate PFB txs using inputs
					// we use ranges to ensure the tests are consistence with random
					txs := generatePayForBlobTransactions(
						t,
						testApp,
						enc.TxConfig,
						kr,
						tt.minsize,
						tt.maxsize,
						tt.blobCount,
						false,
						testutil.ChainID,
						accounts[:tt.count],
						user.SetGasLimitAndGasPrice(1_000_000_000, 0.1),
					)
					
					n_blob := len(txs)
					// blob produced must be equal number of account
					// since each account create a single PFB
					require.Equal(t, n_blob, len(accounts[:tt.count]))

					// create 100 send transactions
					sendTxs := testutil.SendTxsWithAccounts(
						t,
						testApp,
						enc.TxConfig,
						kr,
						1000,
						accounts[0],
						accounts[len(accounts)-sendTxCount:],
						testutil.ChainID,
						user.SetGasLimitAndGasPrice(1_000_000, 0.1),
					)
					txs = append(txs, sendTxs...)

					blockTime := time.Now()
					height := testApp.LastBlockHeight() + 1

					resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
						Txs:    coretypes.Txs(txs).ToSliceOfBytes(),
						Time:   blockTime,
						Height: height,
					})
					require.NoError(t, err)

					// check that the square size is smaller than or equal to
					// the specified size
					require.LessOrEqual(t, resp.SquareSize, uint64(size.govMaxSquareSize))

					res, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
						Height:       height,
						DataRootHash: resp.DataRootHash,
						SquareSize:   resp.SquareSize,
						Txs:          resp.Txs,
					},
					)
					require.NoError(t, err)
					require.Equal(t, abci.ResponseProcessProposal_ACCEPT, res.Status)
					// At least all of the send transactions and one blob tx
					// should make it into the block. This should be expected to
					// change if PFB transactions are not separated and put into
					// their own namespace
					require.GreaterOrEqual(t, len(resp.Txs), sendTxCount+1)
					// at this point valid 100 valid txs and 1 blob
					// we check the amount of blob that made it into block
					// we know the rate of inclusion of blob so we can
					// safetly assert that a certain amount of blob made it through
					valid_blob := len(resp.Txs) - sendTxCount

					incl_rate := float64(valid_blob) / float64(n_blob)
					require.GreaterOrEqual(t, incl_rate, tt.rate)
				}
			})
		}
	}
}

// generatePayForBlobTransactions creates a number of valid PFB txs
// for accounts
func generatePayForBlobTransactions(
	t *testing.T,
	testApp *app.App,
	cfg client.TxConfig,
	kr keyring.Keyring,
	minsize int,
	maxs int,
	blobcount int,
	randomize bool, 
	chainid string,
	accounts []string,
	extraOpts ...user.TxOption,
) []coretypes.Tx {
	opts := append(blobfactory.DefaultTxOpts(), extraOpts...)
	rawTxs := make([]coretypes.Tx, 0, len(accounts))
	for i := range accounts {
		addr := testfactory.GetAddress(kr, accounts[i])
		acc := testutil.DirectQueryAccount(testApp, addr)
		accountSequence := acc.GetSequence()
		account := user.NewAccount(accounts[i], acc.GetAccountNumber(), accountSequence)
		signer, err := user.NewSigner(kr, cfg, chainid, account)
		require.NoError(t, err)
		var count, size int
		if randomize{
			count = randInRange(1, blobcount)
			size = randInRange(minsize, maxs)
		}else{
			if minsize < 0 {
				size = maxs
			} else {
				size = 1
			}
			if blobcount < 0 {
				count = randInRange(1, blobcount)
			} else {
				count = 1
			}
		}
		blobs := make([]*share.Blob, count)
		randomBytes := crypto.CRandBytes(size)
		for i := 0; i < count; i++ {
			blob, err := share.NewBlob(share.RandomNamespace(), randomBytes, 1, acc.GetAddress().Bytes())
			require.NoError(t, err)
			blobs[i] = blob
		}
		// create PFB tx per account
		tx, _, err := signer.CreatePayForBlobs(account.Name(), blobs, opts...)
		require.NoError(t, err)
		rawTxs = append(rawTxs, tx)

	}

	return rawTxs
}

// generate random numbers in specified range
func randInRange(min, max int) int {
    return rand.Intn(max-min+1) + min
}
