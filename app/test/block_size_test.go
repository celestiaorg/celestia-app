package app_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	cosmosnet "github.com/cosmos/cosmos-sdk/testutil/network"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/testutil/network"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// username is used to create a funded genesis account under this name
const username = "test"

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
	rec, err := net.Validators[0].ClientCtx.Keyring.Key(testAccName)
	require.NoError(s.T(), err)
	addr, err := rec.GetAddress()
	require.NoError(s.T(), err)
	fmt.Println(addr.String())

	_, err = s.network.WaitForHeight(1)
	s.Require().NoError(err)
}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	s.network.Cleanup()
}

func (s *IntegrationTestSuite) TestSubmitWirePayForData() {
	require := s.Require()
	val := s.network.Validators[0]

	// some hex message
	// hexMsg := "0204033704032c0b162109000908094d425837422c2116 5555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555"

	encCfg := encoding.MakeEncodingConfig(app.ModuleBasics.RegisterInterfaces)
	signer := types.NewKeyringSigner(val.ClientCtx.Keyring, testAccName, val.ClientCtx.ChainID)

	testCases := []struct {
		name         string
		tx           coretypes.Tx
		expectErr    bool
		expectedCode uint32
		respType     proto.Message
	}{
		{
			"valid transaction",
			generateRawTx(s.T(), encCfg.TxConfig, []byte{1, 2, 3, 4, 5, 6, 7, 8}, bytes.Repeat([]byte{2, 3, 4}, 100), signer, 2, 4, 8, 16, 32, 64),
			false, 0, &sdk.TxResponse{},
		},
	}

	for _, tc := range testCases {
		tc := tc
		s.Require().NoError(s.network.WaitForNextBlock())
		s.Run(tc.name, func() {
			// res, err := val.RPCClient.CheckTx(context.Background(), tc.tx)
			res, err := val.ClientCtx.BroadcastTxCommit(tc.tx)
			require.NoError(err)
			fmt.Println(res.Code, res.Data, res.GasWanted, res.GasUsed, res.Info, res.Events, res.TxHash)

			val.ClientCtx.BroadcastTxCommit(tc.tx)
		})
		s.Require().NoError(s.network.WaitForNextBlock())
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, NewIntegrationTestSuite(network.DefaultConfig()))
}
