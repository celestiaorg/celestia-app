package testutil

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tmcli "github.com/cometbft/cometbft/libs/cli"
	"github.com/stretchr/testify/suite"

	"github.com/celestiaorg/celestia-app/v3/x/mint/client/cli"
	mint "github.com/celestiaorg/celestia-app/v3/x/mint/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
)

type IntegrationTestSuite struct {
	suite.Suite

	cfg  *testnode.Config
	cctx testnode.Context
}

func NewIntegrationTestSuite(cfg *testnode.Config) *IntegrationTestSuite {
	return &IntegrationTestSuite{cfg: cfg}
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.T().Log("setting up x/mint integration test suite")

	s.cctx, _, _ = testnode.NewNetwork(s.T(), s.cfg)

	_, err := s.cctx.WaitForHeight(1)
	s.Require().NoError(err)
}

func (s *IntegrationTestSuite) jsonArgs() []string {
	return []string{fmt.Sprintf("--%s=1", flags.FlagHeight), fmt.Sprintf("--%s=json", tmcli.OutputFlag)}
}

func (s *IntegrationTestSuite) textArgs() []string {
	return []string{fmt.Sprintf("--%s=1", flags.FlagHeight), fmt.Sprintf("--%s=text", tmcli.OutputFlag)}
}

// TestGetCmdQueryInflationRate tests that the CLI query command for inflation
// rate returns the correct value. This test assumes that the initial inflation
// rate is 0.08.
func (s *IntegrationTestSuite) TestGetCmdQueryInflationRate() {
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
		s.Run(tc.name, func() {
			cmd := cli.GetCmdQueryInflationRate()

			got, err := clitestutil.ExecTestCLICmd(s.cctx.Context, cmd, tc.args)
			s.Require().NoError(err)
			s.Require().Equal(tc.want, strings.TrimSpace(got.String()))
		})
	}
}

// TestGetCmdQueryAnnualProvisions tests that the CLI query command for annual-provisions
// returns the correct value. This test assumes that the initial inflation
// rate is 0.08 and the initial total supply is 500_000_000 utia.
//
// TODO assert that total supply is 500_000_000 utia.
func (s *IntegrationTestSuite) TestGetCmdQueryAnnualProvisions() {
	testCases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "json output",
			args: s.jsonArgs(),
		},
		{
			name: "text output",
			args: s.textArgs(),
		},
	}

	expectedAnnualProvision := mint.InitialInflationRateAsDec().MulInt(sdk.NewInt(testnode.DefaultInitialBalance))
	for _, tc := range testCases {
		s.Run(tc.name, func() {
			cmd := cli.GetCmdQueryAnnualProvisions()
			out, err := clitestutil.ExecTestCLICmd(s.cctx.Context, cmd, tc.args)
			s.Require().NoError(err)

			s.Require().Equal(expectedAnnualProvision.String(), strings.TrimSpace(out.String()))
		})
	}
}

// TestGetCmdQueryGenesisTime tests that the CLI command for genesis time
// returns the same time that is set in the genesis state. The CLI command to
// query genesis time looks like: `celestia-appd query mint genesis-time`
func (s *IntegrationTestSuite) TestGetCmdQueryGenesisTime() {
	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "json output",
			args: s.jsonArgs(),
		},
		{
			name: "text output",
			args: s.textArgs(),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			cmd := cli.GetCmdQueryGenesisTime()
			out, err := clitestutil.ExecTestCLICmd(s.cctx.Context, cmd, tc.args)
			s.Require().NoError(err)

			trimmed := strings.TrimSpace(out.String())
			layout := "2006-01-02 15:04:05.999999 -0700 UTC"

			got, err := time.Parse(layout, trimmed)
			s.Require().NoError(err)

			now := time.Now()
			oneMinForward := now.Add(time.Minute)
			oneMinBackward := now.Add(-time.Minute)

			s.Assert().True(got.Before(oneMinForward), "genesis time is too far in the future")
			s.Assert().True(got.After(oneMinBackward), "genesis time is too far in the past")
		})
	}
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestMintIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestMintIntegrationTestSuite in short mode.")
	}
	cfg := testnode.DefaultConfig()
	suite.Run(t, NewIntegrationTestSuite(cfg))
}
