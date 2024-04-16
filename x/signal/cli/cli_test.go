package cli_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v2/x/signal/cli"
	testutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	"github.com/stretchr/testify/suite"
)

func TestCLITestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping upgrade CLI test in short mode")
	}
	suite.Run(t, new(CLITestSuite))
}

type CLITestSuite struct {
	suite.Suite

	ctx testnode.Context
}

func (s *CLITestSuite) SetupSuite() {
	s.T().Log("setting up CLI test suite")
	ctx, _, _ := testnode.NewNetwork(s.T(), testnode.DefaultConfig())
	s.ctx = ctx
	_, err := s.ctx.WaitForHeight(1)
	s.Require().NoError(err)
}

func (s *CLITestSuite) TestCmdQueryTally() {
	cmd := cli.CmdQueryTally()
	output, err := testutil.ExecTestCLICmd(s.ctx.Context, cmd, []string{"1"})
	s.Require().NoError(err)
	s.Require().Contains(output.String(), "voting_power")
	s.Require().Contains(output.String(), "threshold_power")
	s.Require().Contains(output.String(), "total_voting_power")
}
