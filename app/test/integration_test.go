package app_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cosmosnet "github.com/cosmos/cosmos-sdk/testutil/network"

	"github.com/stretchr/testify/suite"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/test/util/network"
	"github.com/celestiaorg/celestia-app/x/blob"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestIntegrationTestSuite(t *testing.T) {
	cfg := network.DefaultConfig()
	cfg.EnableTMLogging = false
	cfg.MinGasPrices = "0utia"
	cfg.NumValidators = 1
	cfg.TargetHeightDuration = time.Millisecond * 400
	suite.Run(t, NewIntegrationTestSuite(cfg))
}

type IntegrationTestSuite struct {
	suite.Suite

	cfg      cosmosnet.Config
	encCfg   encoding.Config
	network  *cosmosnet.Network
	kr       keyring.Keyring
	accounts []string
}

func NewIntegrationTestSuite(cfg cosmosnet.Config) *IntegrationTestSuite {
	return &IntegrationTestSuite{cfg: cfg}
}

func (s *IntegrationTestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skipping app/test/integration_test in short mode.")
	}
	s.T().Log("setting up integration test suite")

	numAccounts := 120
	s.accounts = make([]string, numAccounts)
	for i := 0; i < numAccounts; i++ {
		s.accounts[i] = tmrand.Str(20)
	}

	net := network.New(s.T(), s.cfg, s.accounts...)

	err := network.GRPCConn(net)
	s.Require().NoError(err)

	s.network = net
	s.kr = net.Validators[0].ClientCtx.Keyring
	s.encCfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)

	_, err = s.network.WaitForHeight(1)
	s.Require().NoError(err)
}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	s.network.Cleanup()
}

// WaitForBlocks waits for blockCount number of blocks to be added to the chain.
func (s *IntegrationTestSuite) WaitForBlocks(blockCount int) {
	for i := 0; i < blockCount; i++ {
		err := s.network.WaitForNextBlock()
		s.Require().NoError(err)
	}
}

func (s *IntegrationTestSuite) TestSubmitPayForBlob() {
	require := s.Require()
	val := s.network.Validators[0]
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))

	mustNewBlob := func(ns appns.Namespace, data []byte, shareVersion uint8) *blobtypes.Blob {
		b, err := blobtypes.NewBlob(ns, data, shareVersion)
		require.NoError(err)
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
			[]types.TxBuilderOption{
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
			s.WaitForBlocks(3)

			signer := blobtypes.NewKeyringSigner(s.kr, s.accounts[0], val.ClientCtx.ChainID)
			res, err := blob.SubmitPayForBlob(context.TODO(), signer, val.ClientCtx.GRPCClient, []*blobtypes.Blob{tc.blob, tc.blob}, tc.opts...)
			require.NoError(err)
			require.NotNil(res)
			require.Equal(abci.CodeTypeOK, res.Code, res.Logs)
		})
	}
}

func (s *IntegrationTestSuite) TestUnwrappedPFBRejection() {
	t := s.T()
	val := s.network.Validators[0]

	blobTx := blobfactory.RandBlobTxsWithAccounts(
		s.cfg.TxConfig.TxEncoder(),
		s.kr,
		val.ClientCtx.GRPCClient,
		int(100000),
		1,
		false,
		s.cfg.ChainID,
		s.accounts[:1],
	)

	btx, isBlob := coretypes.UnmarshalBlobTx(blobTx[0])
	require.True(t, isBlob)

	res, err := val.ClientCtx.BroadcastTxSync(btx.Tx)
	require.NoError(t, err)
	require.Equal(t, blobtypes.ErrNoBlobs.ABCICode(), res.Code)
}

func (s *IntegrationTestSuite) TestShareInclusionProof() {
	require := s.Require()
	val := s.network.Validators[0]

	// generate 100 randomly sized txs (max size == 100kb)
	txs := blobfactory.RandBlobTxsWithAccounts(
		s.cfg.TxConfig.TxEncoder(),
		s.kr,
		val.ClientCtx.GRPCClient,
		100000,
		1,
		true,
		s.cfg.ChainID,
		s.accounts[:20],
	)

	hashes := make([]string, len(txs))

	for i, tx := range txs {
		res, err := val.ClientCtx.BroadcastTxSync(tx)
		require.NoError(err)
		require.Equal(abci.CodeTypeOK, res.Code)
		hashes[i] = res.TxHash
	}

	s.WaitForBlocks(20)

	for _, hash := range hashes {
		txResp, err := testnode.QueryTx(val.ClientCtx, hash, true)
		require.NoError(err)
		require.Equal(abci.CodeTypeOK, txResp.TxResult.Code)

		node, err := val.ClientCtx.GetNode()
		require.NoError(err)
		blockRes, err := node.Block(context.Background(), &txResp.Height)
		require.NoError(err)

		_, isBlobTx := coretypes.UnmarshalBlobTx(blockRes.Block.Txs[txResp.Index])
		require.True(isBlobTx)

		// get the blob shares
		shareRange, err := square.BlobShareRange(blockRes.Block.Txs.ToSliceOfBytes(), int(txResp.Index), 0)
		require.NoError(err)

		// verify the blob shares proof
		blobProof, err := node.ProveShares(
			context.Background(),
			uint64(txResp.Height),
			uint64(shareRange.Start),
			uint64(shareRange.End),
		)
		require.NoError(err)
		require.NoError(blobProof.Validate(blockRes.Block.DataHash))
	}
}
