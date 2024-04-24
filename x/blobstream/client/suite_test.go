package client_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/stretchr/testify/suite"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

type CLITestSuite struct {
	suite.Suite
	cfg  *testnode.Config
	cctx testnode.Context
}

func (s *CLITestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skipping Blobstream CLI tests in short mode.")
	}

	cfg := testnode.DefaultConfig().WithConsensusParams(app.DefaultInitialConsensusParams())

	numAccounts := 120
	accounts := make([]string, numAccounts)
	for i := 0; i < numAccounts; i++ {
		accounts[i] = tmrand.Str(20)
	}
	cfg.WithFundedAccounts(accounts...)

	s.cfg = cfg
	s.cctx, _, _ = testnode.NewNetwork(s.T(), cfg)

	height, err := s.cctx.WaitForHeight(2)
	s.Require().NoError(err)
	s.T().Log("waited for height", height)
}

func TestBlobstreamCLI(t *testing.T) {
	suite.Run(t, new(CLITestSuite))
}
