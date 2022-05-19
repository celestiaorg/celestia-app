package app_test

import (
	"bytes"
	"fmt"
	"math/bits"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/suite"

	cosmosnet "github.com/cosmos/cosmos-sdk/testutil/network"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/testutil/network"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/tendermint/tendermint/pkg/consts"
	coretypes "github.com/tendermint/tendermint/types"
)

type IntegrationTestSuite struct {
	suite.Suite

	cfg     cosmosnet.Config
	network *cosmosnet.Network
	kr      keyring.Keyring
}

func NewIntegrationTestSuite(cfg cosmosnet.Config) *IntegrationTestSuite {
	return &IntegrationTestSuite{cfg: cfg}
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.T().Log("setting up integration test suite")

	if testing.Short() {
		s.T().Skip("skipping test in unit-tests mode.")
	}

	net := network.New(s.T(), s.cfg, testAccName)

	s.network = net
	s.kr = net.Validators[0].ClientCtx.Keyring

	_, err := s.network.WaitForHeight(1)
	s.Require().NoError(err)
}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	s.network.Cleanup()
}

func (s *IntegrationTestSuite) TestSubmitWirePayForData() {
	require := s.Require()
	val := s.network.Validators[0]

	encCfg := encoding.MakeEncodingConfig(app.ModuleBasics.RegisterInterfaces)
	signer := types.NewKeyringSigner(val.ClientCtx.Keyring, testAccName, val.ClientCtx.ChainID)

	// we subtract one share as at least one share will be used for the tx
	maxMsgBytes := (24) * consts.MsgShareSize
	delimMaxMsgBytes := maxMsgBytes - delimLen(uint64(maxMsgBytes))
	msg1 := bytes.Repeat([]byte{1}, delimMaxMsgBytes)
	tx1 := generateRawTx(s.T(), encCfg.TxConfig, []byte{1, 2, 3, 4, 5, 6, 7, 8}, msg1, signer, AllSquareSizes(len(msg1))...)
	// signer.SetAccountNumber(0)
	signer.SetSequence(1)
	tx2 := generateRawTx(s.T(), encCfg.TxConfig, []byte{1, 2, 3, 4, 5, 6, 7, 9}, msg1, signer, AllSquareSizes(len(msg1))...)

	testCases := []struct {
		name         string
		txs          []coretypes.Tx
		expectErr    bool
		expectedCode uint32
	}{
		{
			"one message that takes up the entire block",
			[]coretypes.Tx{
				tx1,
				tx2,
			},
			false, 0,
		},
	}

	for _, tc := range testCases {
		tc := tc
		s.Require().NoError(s.network.WaitForNextBlock())
		s.Run(tc.name, func() {

			for _, tx := range tc.txs {
				res, err := val.ClientCtx.BroadcastTxCommit(tx)
				require.NoError(err)
				fmt.Println(res.Code, res.Data, res.Logs, res.RawLog, res.GasWanted, res.GasUsed, res.Info, res.Events, res.TxHash)
				s.Require().NoError(s.network.WaitForNextBlock())
			}

		})
		s.Require().NoError(s.network.WaitForNextBlock())
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	cfg := network.DefaultConfig()
	cfg.EnableTMLogging = false
	cfg.MinGasPrices = "0uceles"
	suite.Run(t, NewIntegrationTestSuite(cfg))
}

// func generateManyRandomSignedWirePayForDataTxs(signer *types.KeyringSigner, startSeq uint64) []sdk.Tx {

// }

// TODO: refactor these into a different package
// they will be useful for
// https://github.com/celestiaorg/celestia-app/issues/236
// https://github.com/celestiaorg/celestia-app/issues/239

// SharesUsed calculates the minimum number of shares a message will take up
func SharesUsed(msgSize int) int {
	shareCount := msgSize / consts.MsgShareSize
	// increment the share count if the message overflows the last counted share
	if msgSize%consts.MsgShareSize != 0 {
		shareCount++
	}
	return shareCount
}

// generateAllSquareSizes generates and returns all of the possible square sizes
// using the maximum and minimum square sizes
func generateAllSquareSizes() []int {
	sizes := []int{}
	cursor := int(consts.MaxSquareSize)
	for cursor >= consts.MinSquareSize {
		sizes = append(sizes, cursor)
		cursor /= 2
	}
	return sizes
}

// AllSquareSizes calculates all of the square sizes that message could possibly
// fit in
func AllSquareSizes(msgSize int) []uint64 {
	allSizes := generateAllSquareSizes()
	fitSizes := []uint64{}
	shareCount := SharesUsed(msgSize)
	for _, size := range allSizes {
		// if the number of shares is larger than that in the square, throw an error
		// note, we use k*k-1 here because at least a single share will be reserved
		// for the transaction paying for the message, therefore the max number of
		// shares a message can be is number of shares in square -1.
		if shareCount > (size*size)-1 {
			continue
		}
		fitSizes = append(fitSizes, uint64(size))
	}
	return fitSizes
}

func delimLen(x uint64) int {
	return 8 - bits.LeadingZeros64(x)%8
}
