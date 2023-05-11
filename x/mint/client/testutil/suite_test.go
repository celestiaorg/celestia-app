package testutil

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	tmcli "github.com/tendermint/tendermint/libs/cli"

	"github.com/celestiaorg/celestia-app/x/mint/client/cli"
	minttypes "github.com/celestiaorg/celestia-app/x/mint/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"

	appnetwork "github.com/celestiaorg/celestia-app/test/util/network"
	"github.com/cosmos/cosmos-sdk/testutil/network"
)

type IntegrationTestSuite struct {
	suite.Suite

	cfg     network.Config
	network *network.Network
}

func NewIntegrationTestSuite(cfg network.Config) *IntegrationTestSuite {
	return &IntegrationTestSuite{cfg: cfg}
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.T().Log("setting up x/mint integration test suite")

	genesisState := s.cfg.GenesisState
	var mintData minttypes.GenesisState
	s.Require().NoError(s.cfg.Codec.UnmarshalJSON(genesisState[minttypes.ModuleName], &mintData))

	var err error
	s.network, err = network.New(s.T(), s.T().TempDir(), s.cfg)
	s.Require().NoError(err)

	_, err = s.network.WaitForHeight(1)
	s.Require().NoError(err)
}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down x/mint integration test suite")
	s.network.Cleanup()
}

func (s *IntegrationTestSuite) jsonArgs() []string {
	return []string{fmt.Sprintf("--%s=1", flags.FlagHeight), fmt.Sprintf("--%s=json", tmcli.OutputFlag)}
}

func (s *IntegrationTestSuite) textArgs() []string {
	return []string{fmt.Sprintf("--%s=1", flags.FlagHeight), fmt.Sprintf("--%s=json", tmcli.OutputFlag)}
}

func (s *IntegrationTestSuite) TestGetCmdQueryInflationRate() {
	val := s.network.Validators[0]

	testCases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "json output",
			args: s.jsonArgs(),
			want: `0.080000000000000000`,
		},
		{
			name: "text output",
			args: s.textArgs(),
			want: `0.080000000000000000`,
		},
	}

	for _, tc := range testCases {
		tc := tc

		s.Run(tc.name, func() {
			cmd := cli.GetCmdQueryInflationRate()
			clientCtx := val.ClientCtx

			got, err := clitestutil.ExecTestCLICmd(clientCtx, cmd, tc.args)
			s.Require().NoError(err)
			s.Require().Equal(tc.want, strings.TrimSpace(got.String()))
		})
	}
}

func (s *IntegrationTestSuite) TestGetCmdQueryAnnualProvisions() {
	val := s.network.Validators[0]

	testCases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "json output",
			args: s.jsonArgs(),
			want: `40000000.000000000000000000`,
		},
		{
			name: "text output",
			args: s.textArgs(),
			want: `40000000.000000000000000000`,
		},
	}

	for _, tc := range testCases {
		tc := tc

		s.Run(tc.name, func() {
			cmd := cli.GetCmdQueryAnnualProvisions()
			clientCtx := val.ClientCtx

			out, err := clitestutil.ExecTestCLICmd(clientCtx, cmd, tc.args)
			s.Require().NoError(err)
			s.Require().Equal(tc.want, strings.TrimSpace(out.String()))
		})
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	cfg := appnetwork.DefaultConfig()
	cfg.NumValidators = 1
	suite.Run(t, NewIntegrationTestSuite(cfg))
}
