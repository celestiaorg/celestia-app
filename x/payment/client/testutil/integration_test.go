package testutil

import (
	"fmt"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/suite"

	"github.com/celestiaorg/celestia-app/testutil/network"
	paycli "github.com/celestiaorg/celestia-app/x/payment/client/cli"
	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	cosmosnet "github.com/cosmos/cosmos-sdk/testutil/network"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const username = "test"

type IntegrationTestSuite struct {
	suite.Suite

	cfg      cosmosnet.Config
	network  *cosmosnet.Network
	kr       keyring.Keyring
	userName string
}

func NewIntegrationTestSuite(cfg cosmosnet.Config) *IntegrationTestSuite {
	return &IntegrationTestSuite{cfg: cfg}
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.T().Log("setting up integration test suite")
	const username = "test"

	if testing.Short() {
		s.T().Skip("skipping test in unit-tests mode.")
	}

	net, kr := network.New(s.T(), s.cfg, username)
	s.network = net
	s.kr = kr
	_, err := s.network.WaitForHeight(1)
	s.Require().NoError(err)

}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	s.network.Cleanup()
}

func (s *IntegrationTestSuite) TestNewCreateValidatorCmd() {
	require := s.Require()
	val := s.network.Validators[0]

	// some hex message
	// some hex namespace
	hexNS := "0102030405060708"
	hexMsg := "0204033704032c0b162109000908094d425837422c2116"

	// use the old code you had for the test where you added funded genesis accounts

	testCases := []struct {
		name         string
		args         []string
		expectErr    bool
		expectedCode uint32
		respType     proto.Message
	}{
		// {
		// 	"invalid transaction (missing moniker)",
		// 	[]string{
		// 		fmt.Sprintf("--%s=%s", cli.FlagPubKey, consPubKeyBz),
		// 		fmt.Sprintf("--%s=%dstake", cli.FlagAmount, 100),
		// 		fmt.Sprintf("--%s=AFAF00C4", cli.FlagIdentity),
		// 		fmt.Sprintf("--%s=https://newvalidator.io", cli.FlagWebsite),
		// 		fmt.Sprintf("--%s=contact@newvalidator.io", cli.FlagSecurityContact),
		// 		fmt.Sprintf("--%s='Hey, I am a new validator. Please delegate!'", cli.FlagDetails),
		// 		fmt.Sprintf("--%s=0.5", cli.FlagCommissionRate),
		// 		fmt.Sprintf("--%s=1.0", cli.FlagCommissionMaxRate),
		// 		fmt.Sprintf("--%s=0.1", cli.FlagCommissionMaxChangeRate),
		// 		fmt.Sprintf("--%s=1", cli.FlagMinSelfDelegation),
		// 		fmt.Sprintf("--%s=%s", flags.FlagFrom, newAddr),
		// 		fmt.Sprintf("--%s=true", flags.FlagSkipConfirmation),
		// 		fmt.Sprintf("--%s=%s", flags.FlagBroadcastMode, flags.BroadcastBlock),
		// 		fmt.Sprintf("--%s=%s", flags.FlagFees, sdk.NewCoins(sdk.NewCoin(s.cfg.BondDenom, sdk.NewInt(10))).String()),
		// 	},
		// 	true, 0, nil,
		// },
		{
			"valid transaction",
			[]string{
				hexNS,
				hexMsg,
				fmt.Sprintf("--from=%s", username),
			},
			false, 0, &sdk.TxResponse{},
		},
	}

	for _, tc := range testCases {
		tc := tc

		s.Run(tc.name, func() {
			cmd := paycli.CmdWirePayForMessage()
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

				// events := txResp.Logs[0].GetEvents()
				// for i := 0; i < len(events); i++ {
				// 	if events[i].GetType() == "create_validator" {
				// 		attributes := events[i].GetAttributes()
				// 		require.Equal(attributes[1].Value, "100stake")
				// 		break
				// 	}
				// }
			}
		})
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, NewIntegrationTestSuite(network.DefaultConfig()))
}
