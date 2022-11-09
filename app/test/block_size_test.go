package app_test

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/suite"

	cosmosnet "github.com/cosmos/cosmos-sdk/testutil/network"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/testutil/network"
	"github.com/celestiaorg/celestia-app/x/payment"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestIntegrationTestSuite(t *testing.T) {
	cfg := network.DefaultConfig()
	cfg.EnableTMLogging = false
	cfg.MinGasPrices = "0utia"
	cfg.NumValidators = 1
	cfg.TimeoutCommit = time.Millisecond * 400
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
	s.T().Log("setting up integration test suite")

	if testing.Short() {
		s.T().Skip("skipping test in unit-tests mode.")
	}

	numAccounts := 100
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

func (s *IntegrationTestSuite) TestMaxBlockSize() {
	require := s.Require()
	assert := s.Assert()
	val := s.network.Validators[0]

	// tendermint's default tx size limit is 1Mb, so we get close to that
	equallySized1MbTxGen := func(c client.Context) []coretypes.Tx {
		equallySized1MbTxs, err := generateSignedWirePayForDataTxs(c, s.cfg.TxConfig, s.kr, 970000, s.accounts[:20]...)
		require.NoError(err)
		return equallySized1MbTxs
	}

	// generate 100 randomly sized txs (max size == 100kb) by generating these
	// transaction using some of the same accounts as the previous genertor, we
	// are also testing to ensure that the sequence number is being utilized
	// corrected in malleated txs
	randoTxGen := func(c client.Context) []coretypes.Tx {
		randomTxs, err := generateSignedWirePayForDataTxs(c, s.cfg.TxConfig, s.kr, -1, s.accounts...)
		require.NoError(err)
		return randomTxs
	}

	type test struct {
		name        string
		txGenerator func(clientCtx client.Context) []coretypes.Tx
	}

	tests := []test{
		{
			"20 ~1Mb txs",
			equallySized1MbTxGen,
		},
		{
			"100 random txs",
			randoTxGen,
		},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			txs := tc.txGenerator(val.ClientCtx)
			hashes := make([]string, len(txs))

			for i, tx := range txs {
				res, err := val.ClientCtx.BroadcastTxSync(tx)
				require.NoError(err)
				require.Equal(abci.CodeTypeOK, res.Code)
				hashes[i] = res.TxHash
			}

			// wait a few blocks to clear the txs
			for i := 0; i < 16; i++ {
				require.NoError(s.network.WaitForNextBlock())
			}

			heights := make(map[int64]int)
			for _, hash := range hashes {
				// TODO: reenable fetching and verifying proofs
				resp, err := queryTx(val.ClientCtx, hash, false)
				assert.NoError(err)
				assert.Equal(abci.CodeTypeOK, resp.TxResult.Code)
				if resp.TxResult.Code == abci.CodeTypeOK {
					heights[resp.Height]++
				}
				// require.True(resp.Proof.VerifyProof())
			}

			require.Greater(len(heights), 0)

			sizes := []uint64{}
			// check the square size
			for height := range heights {
				node, err := val.ClientCtx.GetNode()
				require.NoError(err)
				blockRes, err := node.Block(context.Background(), &height)
				require.NoError(err)
				size := blockRes.Block.Data.OriginalSquareSize

				// perform basic checks on the size of the square
				assert.LessOrEqual(size, uint64(appconsts.MaxSquareSize))
				assert.GreaterOrEqual(size, uint64(appconsts.MinSquareSize))
				sizes = append(sizes, size)
			}

			// ensure that at least one of the blocks used the max square size
			assert.Contains(sizes, uint64(appconsts.MaxSquareSize))
		})
		require.NoError(s.network.WaitForNextBlock())
	}
}

func (s *IntegrationTestSuite) TestSubmitWirePayForData() {
	require := s.Require()
	assert := s.Assert()
	val := s.network.Validators[0]

	type test struct {
		name    string
		ns      []byte
		message []byte
		opts    []types.TxBuilderOption
	}

	tests := []test{
		{
			"small random typical",
			[]byte{1, 2, 3, 4, 5, 6, 7, 8},
			tmrand.Bytes(3000),
			[]types.TxBuilderOption{
				types.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1)))),
			},
		},
		{
			"large random typical",
			[]byte{2, 3, 4, 5, 6, 7, 8, 9},
			tmrand.Bytes(700000),
			[]types.TxBuilderOption{
				types.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(10)))),
			},
		},
		{
			"medium random with memo",
			[]byte{2, 3, 4, 5, 6, 7, 8, 9},
			tmrand.Bytes(100000),
			[]types.TxBuilderOption{
				types.SetMemo("lol I could stick the rollup block here if I wanted to"),
			},
		},
		{
			"medium random with timeout height",
			[]byte{2, 3, 4, 5, 6, 7, 8, 9},
			tmrand.Bytes(100000),
			[]types.TxBuilderOption{
				types.SetTimeoutHeight(1000),
			},
		},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			signer := types.NewKeyringSigner(s.kr, s.accounts[0], val.ClientCtx.ChainID)
			res, err := payment.SubmitPayForData(context.TODO(), signer, val.ClientCtx.GRPCClient, tc.ns, tc.message, 10000000, tc.opts...)
			require.NoError(err)
			require.NotNil(res)
			assert.Equal(abci.CodeTypeOK, res.Code)
			// occasionally this test will error that the mempool is full (code
			// 20) so we wait a few blocks for the txs to clear
			for i := 0; i < 3; i++ {
				require.NoError(s.network.WaitForNextBlock())
			}
		})
	}
}

func generateSignedWirePayForDataTxs(clientCtx client.Context, txConfig client.TxConfig, kr keyring.Keyring, msgSize int, accounts ...string) ([]coretypes.Tx, error) {
	txs := make([]coretypes.Tx, len(accounts))
	for i, account := range accounts {
		signer := types.NewKeyringSigner(kr, account, clientCtx.ChainID)

		err := signer.UpdateAccountFromClient(clientCtx)
		if err != nil {
			return nil, err
		}

		coin := sdk.Coin{
			Denom:  app.BondDenom,
			Amount: sdk.NewInt(1000000),
		}

		opts := []types.TxBuilderOption{
			types.SetFeeAmount(sdk.NewCoins(coin)),
			types.SetGasLimit(1000000000),
		}

		thisMessageSize := msgSize
		if msgSize < 1 {
			for {
				thisMessageSize = tmrand.NewRand().Intn(100000)
				if thisMessageSize != 0 {
					break
				}
			}
		}

		// create a msg
		msg, err := types.NewWirePayForData(
			namespace.RandomMessageNamespace(),
			tmrand.Bytes(thisMessageSize),
			types.AllSquareSizes(thisMessageSize)...,
		)
		if err != nil {
			return nil, err
		}

		err = msg.SignShareCommitments(signer, opts...)
		if err != nil {
			return nil, err
		}

		builder := signer.NewTxBuilder(opts...)

		tx, err := signer.BuildSignedTx(builder, msg)
		if err != nil {
			return nil, err
		}

		rawTx, err := txConfig.TxEncoder()(tx)
		if err != nil {
			return nil, err
		}

		txs[i] = coretypes.Tx(rawTx)
	}

	return txs, nil
}

func queryTx(clientCtx client.Context, hashHexStr string, prove bool) (*rpctypes.ResultTx, error) {
	hash, err := hex.DecodeString(hashHexStr)
	if err != nil {
		return nil, err
	}

	node, err := clientCtx.GetNode()
	if err != nil {
		return nil, err
	}

	return node.Tx(context.Background(), hash, prove)
}
