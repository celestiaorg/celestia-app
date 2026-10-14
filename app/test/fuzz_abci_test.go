package app_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/random"
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
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

// TestPrepareProposalConsistencyCorpus checks that a validator with the same
// committed state accepts every proposal built from a small fixed corpus.
func TestPrepareProposalConsistencyCorpus(t *testing.T) {
	accounts := []string{"a0", "a1", "a2", "a3"}
	blockTime := testutil.GenesisTime.Add(time.Minute)

	type testCase struct {
		name       string
		txs        [][]byte
		maxTxBytes int64
		want       [][]byte
	}
	run := func(t *testing.T, proposer, validator *app.App, tc testCase) {
		height := proposer.LastBlockHeight() + 1
		maxTxBytes := tc.maxTxBytes
		if maxTxBytes == 0 {
			maxTxBytes = appconsts.BlockMaxBytes
		}
		prepare, err := proposer.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:        tc.txs,
			Height:     height,
			Time:       blockTime,
			MaxTxBytes: maxTxBytes,
		})
		require.NoError(t, err)
		require.Equal(t, tc.want, prepare.Txs)

		process, err := validator.ProcessProposal(&abci.RequestProcessProposal{
			Height:       height,
			Time:         blockTime,
			Txs:          prepare.Txs,
			SquareSize:   prepare.SquareSize,
			DataRootHash: prepare.DataRootHash,
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, process.Status)
	}

	t.Run("default square", func(t *testing.T) {
		proposer, validator, txs := newProposalAppPair(t, appconsts.DefaultGovMaxSquareSize, accounts)

		valid := [][]byte{txs.send("a0", 0), txs.send("a1", 0), txs.pfb("a2", 0, 100)}
		framed := coretypes.ComputeProtoSizeForTxs(coretypes.ToTxs(valid))

		badSignature := txs.send("a3", 0)
		badSignature[len(badSignature)-1]++
		malformed := [][]byte{
			nil,
			{},
			[]byte("not a tx"),
			valid[0][:len(valid[0])/2],
			valid[2][:len(valid[2])/2],
			badSignature,
			appendUnknownProtoField(valid[2], 16),
		}

		maxSDKMsgs := make([]sdk.Msg, appconsts.MaxSDKMessages)
		for i := range maxSDKMsgs {
			maxSDKMsgs[i] = txs.sendMsg("a0")
		}
		maxSDKTx := txs.signed("a0", 0, maxSDKMsgs...)

		pfbs := make([][]byte, appconsts.MaxPFBMessages+1)
		for i := range pfbs {
			pfbs[i] = txs.pfb("a0", uint64(i), 1)
		}

		for _, tc := range []testCase{
			{name: "empty block", want: [][]byte{}},
			{
				name: "mixed sends and blob txs",
				txs:  [][]byte{txs.pfb("a0", 0, 100), txs.send("a1", 0), txs.pfb("a2", 0, 1000, 5000), txs.send("a3", 0)},
				want: [][]byte{txs.send("a1", 0), txs.send("a3", 0), txs.pfb("a0", 0, 100), txs.pfb("a2", 0, 1000, 5000)},
			},
			{
				name: "sequences out of order",
				txs:  [][]byte{txs.send("a0", 1), txs.send("a0", 0)},
				want: [][]byte{txs.send("a0", 0)},
			},
			{
				name: "send after blob tx from same signer",
				txs:  [][]byte{txs.pfb("a0", 0, 100), txs.send("a0", 1)},
				want: [][]byte{txs.pfb("a0", 0, 100)},
			},
			{
				name: "blob tx after send from same signer",
				txs:  [][]byte{txs.send("a0", 0), txs.pfb("a0", 1, 100)},
				want: [][]byte{txs.send("a0", 0), txs.pfb("a0", 1, 100)},
			},
			{
				name: "duplicate tx",
				txs:  [][]byte{valid[0], valid[0]},
				want: [][]byte{valid[0]},
			},
			{
				name: "malformed candidates",
				txs:  append(malformed, valid[0], valid[2]),
				want: [][]byte{valid[0], valid[2]},
			},
			{name: "max tx bytes fits exactly", txs: valid, maxTxBytes: framed, want: valid},
			{name: "max tx bytes one short", txs: valid, maxTxBytes: framed - 1, want: valid[:2]},
			{
				name: "tx above max tx size",
				txs:  [][]byte{txs.send("a0", 0, user.SetMemo(strings.Repeat("a", appconsts.MaxTxSize))), valid[1]},
				want: [][]byte{valid[1]},
			},
			{name: "max SDK messages", txs: [][]byte{maxSDKTx, valid[1]}, want: [][]byte{maxSDKTx}},
			{name: "max PFB messages", txs: pfbs, want: pfbs[:appconsts.MaxPFBMessages]},
		} {
			t.Run(tc.name, func(t *testing.T) { run(t, proposer, validator, tc) })
		}
	})

	t.Run("small square", func(t *testing.T) {
		const squareSize = 16
		proposer, validator, txs := newProposalAppPair(t, squareSize, accounts)
		tooLarge := squareSize * squareSize * share.ContinuationSparseShareContentSize
		hundredShares := 100 * share.ContinuationSparseShareContentSize

		for _, tc := range []testCase{
			{
				name: "blob larger than square",
				txs:  [][]byte{txs.pfb("a0", 0, tooLarge), txs.send("a1", 0)},
				want: [][]byte{txs.send("a1", 0)},
			},
			{
				name: "square fills up",
				txs:  [][]byte{txs.pfb("a0", 0, hundredShares), txs.pfb("a1", 0, hundredShares), txs.pfb("a2", 0, hundredShares)},
				want: [][]byte{txs.pfb("a0", 0, hundredShares), txs.pfb("a1", 0, hundredShares)},
			},
		} {
			t.Run(tc.name, func(t *testing.T) { run(t, proposer, validator, tc) })
		}
	})
}

// corpusTxs signs deterministic txs for the corpus accounts.
type corpusTxs struct {
	t      *testing.T
	signer *user.Signer
}

func (c corpusTxs) sendMsg(from string) sdk.Msg {
	addr := c.signer.Account(from).Address()
	return banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
}

func (c corpusTxs) blobs(sizes ...int) []*share.Blob {
	blobs := make([]*share.Blob, len(sizes))
	for i, size := range sizes {
		namespace := share.MustNewV0Namespace(bytes.Repeat([]byte{byte(i + 1)}, share.NamespaceVersionZeroIDSize))
		blob, err := share.NewBlob(namespace, bytes.Repeat([]byte{byte(i + 1)}, size), appconsts.DefaultShareVersion, nil)
		require.NoError(c.t, err)
		blobs[i] = blob
	}
	return blobs
}

func (c corpusTxs) signed(from string, seq uint64, msgs ...sdk.Msg) []byte {
	require.NoError(c.t, c.signer.SetSequence(from, seq))
	tx, _, err := c.signer.CreateTx(msgs, user.SetGasLimitAndGasPrice(100_000_000, 0.1))
	require.NoError(c.t, err)
	return tx
}

func (c corpusTxs) send(from string, seq uint64, opts ...user.TxOption) []byte {
	require.NoError(c.t, c.signer.SetSequence(from, seq))
	opts = append([]user.TxOption{user.SetGasLimitAndGasPrice(1_000_000, 0.1)}, opts...)
	tx, _, err := c.signer.CreateTx([]sdk.Msg{c.sendMsg(from)}, opts...)
	require.NoError(c.t, err)
	return tx
}

func (c corpusTxs) pfb(from string, seq uint64, sizes ...int) []byte {
	require.NoError(c.t, c.signer.SetSequence(from, seq))
	tx, _, err := c.signer.CreatePayForBlobs(from, c.blobs(sizes...), user.SetGasLimitAndGasPrice(10_000_000, 0.1))
	require.NoError(c.t, err)
	return tx
}

// newProposalAppPair returns a proposer and a validator app with identical
// committed state, plus a tx builder for the funded accounts.
func newProposalAppPair(t *testing.T, govMaxSquareSize uint64, accounts []string) (*app.App, *app.App, corpusTxs) {
	proposer, validator := testutil.NewTestApp(), testutil.NewTestApp()
	genesis, valSet, kr := testutil.GenesisStateWithSingleValidator(proposer, accounts...)
	var blobGenesis blobtypes.GenesisState
	proposer.AppCodec().MustUnmarshalJSON(genesis[blobtypes.ModuleName], &blobGenesis)
	blobGenesis.Params.GovMaxSquareSize = govMaxSquareSize
	genesis[blobtypes.ModuleName] = proposer.AppCodec().MustMarshalJSON(&blobGenesis)

	for _, a := range []*app.App{proposer, validator} {
		testutil.InitialiseTestAppWithGenesis(a, app.DefaultConsensusParams(), genesis)
		_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{
			Time:               testutil.GenesisTime,
			Height:             a.LastBlockHeight() + 1,
			Hash:               a.LastCommitID().Hash,
			NextValidatorsHash: valSet.Hash(),
		})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, a.Close()) })
	}
	require.Equal(t, proposer.LastCommitID(), validator.LastCommitID())

	signerAccounts := make([]*user.Account, len(accounts))
	for i, info := range queryAccountInfo(proposer, accounts, kr) {
		signerAccounts[i] = user.NewAccount(accounts[i], info.AccountNum, info.Sequence)
	}
	signer, err := user.NewSigner(kr, proposer.GetTxConfig(), testutil.ChainID, signerAccounts...)
	require.NoError(t, err)
	return proposer, validator, corpusTxs{t: t, signer: signer}
}
