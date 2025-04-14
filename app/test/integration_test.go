package app_test

import (
	"bytes"
	"context"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	square "github.com/celestiaorg/go-square/v2"
	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/random"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
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
	s.accounts = testnode.RandomAccounts(142)

	cfg := testnode.DefaultConfig().WithFundedAccounts(s.accounts...)

	cctx, _, _ := testnode.NewNetwork(t, cfg)

	s.cctx = cctx
	s.ecfg = encoding.MakeTestConfig(app.ModuleEncodingRegisters...)

	require.NoError(t, cctx.WaitForNextBlock())

	for _, acc := range s.accounts {
		addr := testfactory.GetAddress(s.cctx.Keyring, acc)
		_, _, err := user.QueryAccount(s.cctx.GoContext(), s.cctx.GRPCClient, s.cctx.InterfaceRegistry, addr)
		require.NoError(t, err)
	}
}

func (s *IntegrationTestSuite) TestMaxBlockSize() {
	t := s.T()

	singleBlobTxGen := func(c client.Context) []coretypes.Tx {
		return blobfactory.RandBlobTxsWithAccounts(
			s.ecfg,
			random.New(),
			s.cctx.Keyring,
			c.GRPCClient,
			600*kibibyte,
			1,
			false,
			s.accounts[:20],
		)
	}

	// This tx generator generates txs that contain 3 blobs each of 200 KiB so
	// 600 KiB total per transaction.
	multiBlobTxGen := func(c client.Context) []coretypes.Tx {
		return blobfactory.RandBlobTxsWithAccounts(
			s.ecfg,
			random.New(),
			s.cctx.Keyring,
			c.GRPCClient,
			200*kibibyte,
			3,
			false,
			s.accounts[20:40],
		)
	}

	randomTxGen := func(c client.Context) []coretypes.Tx {
		return blobfactory.RandBlobTxsWithAccounts(
			s.ecfg,
			random.New(),
			s.cctx.Keyring,
			c.GRPCClient,
			50*kibibyte,
			8,
			true,
			s.accounts[40:120],
		)
	}

	type test struct {
		name        string
		txGenerator func(clientCtx client.Context) []coretypes.Tx
	}
	tests := []test{
		{"singleBlobTxGen", singleBlobTxGen},
		{"multiBlobTxGen", multiBlobTxGen},
		{"randomTxGen", randomTxGen},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			txs := tc.txGenerator(s.cctx.Context)
			hashes := make([]string, len(txs))

			for i, tx := range txs {
				// The default CometBFT mempool MaxTxBytes is 1 MiB so the generators in
				// this test must create transactions that are smaller than that.
				require.LessOrEqual(t, len(tx), 1*mebibyte)

				res, err := s.cctx.BroadcastTxSync(tx)
				require.NoError(t, err)
				assert.Equal(t, abci.CodeTypeOK, res.Code, res.RawLog)
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
				require.NotNil(t, resp)
				require.Equal(t, abci.CodeTypeOK, resp.TxResult.Code, resp.TxResult.Log)
				heights[resp.Height]++
				// ensure that some gas was used
				require.GreaterOrEqual(t, resp.TxResult.GasUsed, int64(10))
			}

			require.Greater(t, len(heights), 0)

			sizes := []uint64{}
			// check the square size
			for height := range heights {
				node, err := s.cctx.GetNode()
				require.NoError(t, err)
				blockRes, err := node.Block(context.Background(), &height)
				require.NoError(t, err)
				size := blockRes.Block.SquareSize

				// perform basic checks on the size of the square
				require.LessOrEqual(t, size, uint64(appconsts.DefaultGovMaxSquareSize))
				require.GreaterOrEqual(t, size, uint64(appconsts.MinSquareSize))

				require.EqualValues(t, appconsts.LatestVersion, blockRes.Block.Version.App)

				sizes = append(sizes, size)
			}
			// ensure that at least one of the blocks used the max square size
			assert.Contains(t, sizes, uint64(appconsts.DefaultGovMaxSquareSize))
		})
		require.NoError(t, s.cctx.WaitForNextBlock())
	}
}

func (s *IntegrationTestSuite) TestUnwrappedPFBRejection() {
	t := s.T()

	blobTx := blobfactory.RandBlobTxsWithAccounts(
		s.ecfg,
		random.New(),
		s.cctx.Keyring,
		s.cctx.GRPCClient,
		int(100000),
		1,
		false,
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

	txs := blobfactory.RandBlobTxsWithAccounts(
		s.ecfg,
		random.New(),
		s.cctx.Keyring,
		s.cctx.GRPCClient,
		100*kibibyte,
		1,
		true,
		s.accounts[120:140],
	)

	hashes := make([]string, len(txs))

	for i, tx := range txs {
		res, err := s.cctx.BroadcastTxSync(tx)
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, res.Code, res.RawLog)
		hashes[i] = res.TxHash
	}

	require.NoError(t, s.cctx.WaitForBlocks(5))

	for _, hash := range hashes {
		txResp, err := testnode.QueryTx(s.cctx.Context, hash, true)
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, txResp.TxResult.Code)

		node, err := s.cctx.GetNode()
		require.NoError(t, err)
		blockRes, err := node.Block(context.Background(), &txResp.Height)
		require.NoError(t, err)

		require.EqualValues(t, appconsts.LatestVersion, blockRes.Block.Version.App)

		_, isBlobTx := coretypes.UnmarshalBlobTx(blockRes.Block.Txs[txResp.Index])
		require.True(t, isBlobTx)

		// get the blob shares
		shareRange, err := square.BlobShareRange(blockRes.Block.Txs.ToSliceOfBytes(), int(txResp.Index), 0,
			appconsts.DefaultSquareSizeUpperBound,
			appconsts.SubtreeRootThreshold,
		)
		require.NoError(t, err)

		// verify the blob shares proof
		rpcNode, ok := node.(rpcclient.SignClient)
		require.True(t, ok)
		blobProof, err := rpcNode.ProveSharesV2(
			context.Background(),
			uint64(txResp.Height),
			uint64(shareRange.Start),
			uint64(shareRange.End),
		)
		require.NoError(t, err)
		require.NoError(t, blobProof.ShareProof.Validate(blockRes.Block.DataHash.Bytes()))
	}
}

func newBlobWithSize(size int) *share.Blob {
	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	data := random.Bytes(size)
	blob, err := share.NewBlob(ns, data, share.ShareVersionZero, nil)
	if err != nil {
		panic(err)
	}
	return blob
}
