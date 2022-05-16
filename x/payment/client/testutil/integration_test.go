package testutil

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/pkg/consts"

	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	cosmosnet "github.com/cosmos/cosmos-sdk/testutil/network"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/x/payment/types"

	"github.com/celestiaorg/celestia-app/testutil/network"
	paycli "github.com/celestiaorg/celestia-app/x/payment/client/cli"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
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

	net := network.New(s.T(), s.cfg, username)

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

	// some hex namespace
	hexNS := "0102030405060708"
	// some hex message
	hexMsg := "0204033704032c0b162109000908094d425837422c2116"

	testCases := []struct {
		name         string
		args         []string
		expectErr    bool
		expectedCode uint32
		respType     proto.Message
	}{
		{
			"valid transaction",
			[]string{
				hexNS,
				hexMsg,
				fmt.Sprintf("--from=%s", username),
				fmt.Sprintf("--%s=%s", flags.FlagBroadcastMode, flags.BroadcastBlock),
				fmt.Sprintf("--%s=%s", flags.FlagFees, sdk.NewCoins(sdk.NewCoin(s.cfg.BondDenom, sdk.NewInt(2))).String()),
				fmt.Sprintf("--%s=true", flags.FlagSkipConfirmation),
				fmt.Sprintf("--%s=%s", paycli.FlagSquareSizes, "2"),
			},
			false, 0, &sdk.TxResponse{},
		},
		{
			"valid transaction list of square sizes",
			[]string{
				hexNS,
				hexMsg,
				fmt.Sprintf("--from=%s", username),
				fmt.Sprintf("--%s=%s", flags.FlagBroadcastMode, flags.BroadcastBlock),
				fmt.Sprintf("--%s=%s", flags.FlagFees, sdk.NewCoins(sdk.NewCoin(s.cfg.BondDenom, sdk.NewInt(2))).String()),
				fmt.Sprintf("--%s=true", flags.FlagSkipConfirmation),
				fmt.Sprintf("--%s=%s", paycli.FlagSquareSizes, "2,4,8,16,32"),
			},
			false, 0, &sdk.TxResponse{},
		},
		{
			"invalid transaction list of square sizes",
			[]string{
				hexNS,
				hexMsg,
				fmt.Sprintf("--from=%s", username),
				fmt.Sprintf("--%s=%s", flags.FlagBroadcastMode, flags.BroadcastBlock),
				fmt.Sprintf("--%s=%s", flags.FlagFees, sdk.NewCoins(sdk.NewCoin(s.cfg.BondDenom, sdk.NewInt(2))).String()),
				fmt.Sprintf("--%s=true", flags.FlagSkipConfirmation),
				fmt.Sprintf("--%s=%s", paycli.FlagSquareSizes, "256,123,64"),
			},
			true, 0, &sdk.TxResponse{},
		},
	}

	for _, tc := range testCases {
		tc := tc
		s.Require().NoError(s.network.WaitForNextBlock())
		s.Run(tc.name, func() {
			cmd := paycli.CmdWirePayForData()
			clientCtx := val.ClientCtx

			out, err := clitestutil.ExecTestCLICmd(clientCtx, cmd, tc.args)
			if tc.expectErr {
				require.Error(err)
			} else {
				require.NoError(err, "test: %s\noutput: %s", tc.name, out.String())
				err = clientCtx.Codec.UnmarshalJSON(out.Bytes(), tc.respType)
				require.NoError(err, out.String(), "test: %s, output\n:", tc.name, out.String())

				txResp := tc.respType.(*sdk.TxResponse)
				require.Equal(tc.expectedCode, txResp.Code,
					"test: %s, output\n:", tc.name, out.String())

				events := txResp.Logs[0].GetEvents()
				for _, e := range events {
					switch e.Type {
					case types.EventTypePayForData:
						signer := e.GetAttributes()[0].GetValue()
						_, err = sdk.AccAddressFromBech32(signer)
						require.NoError(err)
						msgSize, err := strconv.ParseUint(e.GetAttributes()[1].GetValue(), 10, 64)
						require.NoError(err)
						s.Equal(uint64(0), msgSize%consts.ShareSize, "Message length should be multiples of const.ShareSize=%v", consts.ShareSize)
					}
				}

				// wait for the tx to be indexed
				s.Require().NoError(s.network.WaitForNextBlock())

				// attempt to query for the malleated transaction using the original tx's hash
				qTxCmd := authcmd.QueryTxCmd()
				out, err := clitestutil.ExecTestCLICmd(clientCtx, qTxCmd, []string{txResp.TxHash, "--output=json"})
				require.NoError(err)

				var result sdk.TxResponse
				s.Require().NoError(val.ClientCtx.Codec.UnmarshalJSON(out.Bytes(), &result))
			}
		})
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, NewIntegrationTestSuite(network.DefaultConfig()))
}
