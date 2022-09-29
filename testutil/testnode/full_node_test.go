package testnode

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/config"
)

type IntegrationTestSuite struct {
	suite.Suite

	cleanups []func()
	cctx     client.Context
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.T().Log("setting up integration test suite")
	require := s.Require()

	tmCfg := config.DefaultConfig()
	tmCfg.Consensus.TimeoutCommit = time.Millisecond * 100
	tmNode, app, cctx, err := New(s.T(), tmCfg, false, "taco", "salad")
	require.NoError(err)

	cctx, stopNode, err := StartNode(tmNode, cctx)
	require.NoError(err)
	s.cleanups = append(s.cleanups, stopNode)

	cctx, cleanupGRPC, err := StartGRPCServer(app, *DefaultAppConfig(), cctx)
	s.cleanups = append(s.cleanups, cleanupGRPC)

	s.cctx = cctx
}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	for _, c := range s.cleanups {
		c()
	}
}

func (s *IntegrationTestSuite) TestNetwork_Liveness() {
	require := s.Require()
	_, err := WaitForHeight(s.cctx, 20)
	require.NoError(err)
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}
