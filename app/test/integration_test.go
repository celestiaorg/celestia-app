package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"

	"github.com/stretchr/testify/suite"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/x/blob"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping app/test/integration_test in short mode.")
	}
	suite.Run(t, &IntegrationTestSuite{})
}

type IntegrationTestSuite struct {
	suite.Suite

	ecfg     encoding.Config
	accounts []string
	cctx     testnode.Context
}

func (s *IntegrationTestSuite) SetupSuite() {
	t := s.T()

	numAccounts := 142
	s.accounts = make([]string, numAccounts)
	for i := 0; i < numAccounts; i++ {
		s.accounts[i] = tmrand.Str(20)
	}

	cfg := testnode.DefaultConfig().WithAccounts(s.accounts)

	cctx, _, _ := testnode.NewNetwork(t, cfg)

	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)

	require.NoError(t, cctx.WaitForNextBlock())

	for _, acc := range s.accounts {
		signer := blobtypes.NewKeyringSigner(s.cctx.Keyring, acc, s.cctx.ChainID)
		err := signer.QueryAccountNumber(s.cctx.GoContext(), s.cctx.GRPCClient)
		require.NoError(t, err)
	}
}

func (s *IntegrationTestSuite) TestMaxBlockSize() {
	t := s.T()

	// tendermint's default tx size limit is 1Mb, so we get close to that
	equallySized1MbTxGen := func(c client.Context) []coretypes.Tx {
		return blobfactory.RandBlobTxsWithAccounts(
			s.ecfg.TxConfig.TxEncoder(),
			tmrand.NewRand(),
			s.cctx.Keyring,
			c.GRPCClient,
			950000,
			1,
			false,
			s.cctx.ChainID,
			s.accounts[:20],
		)
	}

	// Tendermint's default tx size limit is 1 MiB, so we get close to that by
	// generating transactions of size 600 KiB because 3 blobs per transaction *
	// 200,000 bytes each = 600,000 total bytes = 600 KiB per transaction.
	randMultiBlob1MbTxGen := func(c client.Context) []coretypes.Tx {
		return blobfactory.RandBlobTxsWithAccounts(
			s.ecfg.TxConfig.TxEncoder(),
			tmrand.NewRand(),
			s.cctx.Keyring,
			c.GRPCClient,
			200000, // 200 KiB
			3,
			false,
			s.cctx.ChainID,
			s.accounts[20:40],
		)
	}

	// Generate 80 randomly sized txs (max size == 50 KiB). Generate these
	// transactions using some of the same accounts as the previous generator to
	// ensure that the sequence number is being utilized correctly in blob
	// txs
	randoTxGen := func(c client.Context) []coretypes.Tx {
		return blobfactory.RandBlobTxsWithAccounts(
			s.ecfg.TxConfig.TxEncoder(),
			tmrand.NewRand(),
			s.cctx.Keyring,
			c.GRPCClient,
			50000,
			8,
			true,
			s.cctx.ChainID,
			s.accounts[40:120],
		)
	}

	type test struct {
		name        string
		txGenerator func(clientCtx client.Context) []coretypes.Tx
	}

	tests := []test{
		{
			"20 1Mb txs",
			equallySized1MbTxGen,
		},
		{
			"20 1Mb multiblob txs",
			randMultiBlob1MbTxGen,
		},
		{
			"80 random txs",
			randoTxGen,
		},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			txs := tc.txGenerator(s.cctx.Context)
			hashes := make([]string, len(txs))

			for i, tx := range txs {
				res, err := s.cctx.Context.BroadcastTxSync(tx)
				require.NoError(t, err)
				assert.Equal(t, abci.CodeTypeOK, res.Code)
				if res.Code != abci.CodeTypeOK {
					continue
				}
				hashes[i] = res.TxHash
			}

			require.NoError(t, s.cctx.WaitForBlocks(10))

			heights := make(map[int64]int)
			for _, hash := range hashes {
				resp, err := testnode.QueryTx(s.cctx.Context, hash, true)
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if resp == nil {
					continue
				}
				require.Equal(t, abci.CodeTypeOK, resp.TxResult.Code, resp.TxResult.Log)
				heights[resp.Height]++
				// ensure that some gas was used
				require.GreaterOrEqual(t, resp.TxResult.GasUsed, int64(10))
			}

			require.Greater(t, len(heights), 0)

			sizes := []uint64{}
			// check the square size
			for height := range heights {
				node, err := s.cctx.Context.GetNode()
				require.NoError(t, err)
				blockRes, err := node.Block(context.Background(), &height)
				require.NoError(t, err)
				size := blockRes.Block.Data.SquareSize

				// perform basic checks on the size of the square
				require.LessOrEqual(t, size, uint64(appconsts.DefaultGovMaxSquareSize))
				require.GreaterOrEqual(t, size, uint64(appconsts.MinSquareSize))

				// assert that the app version is correctly set
				require.Equal(t, appconsts.LatestVersion, blockRes.Block.Header.Version.App)

				sizes = append(sizes, size)
				ExtendBlobTest(t, blockRes.Block)
			}
			// ensure that at least one of the blocks used the max square size
			assert.Contains(t, sizes, uint64(appconsts.DefaultGovMaxSquareSize))
		})
		require.NoError(t, s.cctx.WaitForNextBlock())
	}
}

func (s *IntegrationTestSuite) TestSubmitPayForBlob() {
	t := s.T()
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))

	mustNewBlob := func(ns appns.Namespace, data []byte, shareVersion uint8) *blobtypes.Blob {
		b, err := blobtypes.NewBlob(ns, data, shareVersion)
		require.NoError(t, err)
		return b
	}

	type test struct {
		name string
		blob *blobtypes.Blob
		opts []blobtypes.TxBuilderOption
	}

	tests := []test{
		{
			"small random typical",
			mustNewBlob(ns1, tmrand.Bytes(3000), appconsts.ShareVersionZero),
			[]blobtypes.TxBuilderOption{
				blobtypes.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1)))),
				blobtypes.SetGasLimit(1_000_000_000),
			},
		},
		{
			"large random typical",
			mustNewBlob(ns1, tmrand.Bytes(350000), appconsts.ShareVersionZero),
			[]blobtypes.TxBuilderOption{
				blobtypes.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(10)))),
				blobtypes.SetGasLimit(1_000_000_000),
			},
		},
		{
			"medium random with memo",
			mustNewBlob(ns1, tmrand.Bytes(100000), appconsts.ShareVersionZero),
			[]blobtypes.TxBuilderOption{
				blobtypes.SetMemo("lol I could stick the rollup block here if I wanted to"),
				blobtypes.SetGasLimit(1_000_000_000),
			},
		},
		{
			"medium random with timeout height",
			mustNewBlob(ns1, tmrand.Bytes(100000), appconsts.ShareVersionZero),
			[]blobtypes.TxBuilderOption{
				blobtypes.SetTimeoutHeight(1000),
				blobtypes.SetGasLimit(1_000_000_000),
			},
		},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			// occasionally this test will error that the mempool is full (code
			// 20) so we wait a few blocks for the txs to clear
			require.NoError(t, s.cctx.WaitForBlocks(3))

			signer := blobtypes.NewKeyringSigner(s.cctx.Keyring, s.accounts[141], s.cctx.ChainID)
			res, err := blob.SubmitPayForBlob(context.TODO(), signer, s.cctx.GRPCClient, []*blobtypes.Blob{tc.blob, tc.blob}, tc.opts...)
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Equal(t, abci.CodeTypeOK, res.Code, res.Logs)
		})
	}
}

func (s *IntegrationTestSuite) TestUnwrappedPFBRejection() {
	t := s.T()

	blobTx := blobfactory.RandBlobTxsWithAccounts(
		s.ecfg.TxConfig.TxEncoder(),
		tmrand.NewRand(),
		s.cctx.Keyring,
		s.cctx.GRPCClient,
		int(100000),
		1,
		false,
		s.cctx.ChainID,
		s.accounts[140:],
	)

	btx, isBlob := coretypes.UnmarshalBlobTx(blobTx[0])
	require.True(t, isBlob)

	res, err := s.cctx.BroadcastTxSync(btx.Tx)
	require.NoError(t, err)
	require.Equal(t, blobtypes.ErrNoBlobs.ABCICode(), res.Code)
}

func (s *IntegrationTestSuite) TestShareInclusionProof() {
	t := s.T()

	// generate 100 randomly sized txs (max size == 100kb)
	txs := blobfactory.RandBlobTxsWithAccounts(
		s.ecfg.TxConfig.TxEncoder(),
		tmrand.NewRand(),
		s.cctx.Keyring,
		s.cctx.GRPCClient,
		100000,
		1,
		true,
		s.cctx.ChainID,
		s.accounts[120:140],
	)

	hashes := make([]string, len(txs))

	for i, tx := range txs {
		res, err := s.cctx.Context.BroadcastTxSync(tx)
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, res.Code, res.RawLog)
		hashes[i] = res.TxHash
	}

	require.NoError(t, s.cctx.WaitForBlocks(5))

	for _, hash := range hashes {
		txResp, err := testnode.QueryTx(s.cctx.Context, hash, true)
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, txResp.TxResult.Code)

		node, err := s.cctx.Context.GetNode()
		require.NoError(t, err)
		blockRes, err := node.Block(context.Background(), &txResp.Height)
		require.NoError(t, err)

		require.Equal(t, appconsts.LatestVersion, blockRes.Block.Header.Version.App)

		_, isBlobTx := coretypes.UnmarshalBlobTx(blockRes.Block.Txs[txResp.Index])
		require.True(t, isBlobTx)

		// get the blob shares
		shareRange, err := square.BlobShareRange(blockRes.Block.Txs.ToSliceOfBytes(), int(txResp.Index), 0, appconsts.LatestVersion)
		require.NoError(t, err)

		// verify the blob shares proof
		blobProof, err := node.ProveShares(
			context.Background(),
			uint64(txResp.Height),
			uint64(shareRange.Start),
			uint64(shareRange.End),
		)
		require.NoError(t, err)
		require.NoError(t, blobProof.Validate(blockRes.Block.DataHash))
	}
}

// ExtendBlobTest re-extends the block and compares the data roots to ensure
// that the public functions for extending the block are working correctly.
func ExtendBlobTest(t *testing.T, block *coretypes.Block) {
	eds, err := app.ExtendBlock(block.Data, block.Header.Version.App)
	require.NoError(t, err)
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	if !assert.Equal(t, dah.Hash(), block.DataHash.Bytes()) {
		// save block to json file for further debugging if this occurs
		b, err := json.MarshalIndent(block, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(fmt.Sprintf("bad_block_%s.json", tmrand.Str(6)), b, 0o644))
	}
}

func (s *IntegrationTestSuite) TestEmptyBlock() {
	t := s.T()
	emptyHeights := []int64{1, 2, 3}
	for _, h := range emptyHeights {
		blockRes, err := s.cctx.Client.Block(s.cctx.GoContext(), &h)
		require.NoError(t, err)
		require.True(t, app.IsEmptyBlock(blockRes.Block.Data, blockRes.Block.Header.Version.App))
		ExtendBlobTest(t, blockRes.Block)
	}
}
