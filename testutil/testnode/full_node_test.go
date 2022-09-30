package testnode

import (
	"context"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/config"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

type IntegrationTestSuite struct {
	suite.Suite

	cleanups []func()
	accounts []string
	cctx     client.Context
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.T().Log("setting up integration test suite")
	require := s.Require()

	// we create an arbitray number of funded accounts
	for i := 0; i < 300; i++ {
		s.accounts = append(s.accounts, tmrand.Str(9))
	}

	tmCfg := config.DefaultConfig()
	tmCfg.Consensus.TimeoutCommit = time.Millisecond * 100
	tmNode, app, cctx, err := New(s.T(), tmCfg, false, s.accounts...)
	require.NoError(err)

	cctx, stopNode, err := StartNode(tmNode, cctx)
	require.NoError(err)
	s.cleanups = append(s.cleanups, stopNode)

	cctx, cleanupGRPC, err := StartGRPCServer(app, DefaultAppConfig(), cctx)
	s.cleanups = append(s.cleanups, cleanupGRPC)

	s.cctx = cctx
}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	for _, c := range s.cleanups {
		c()
	}
}

func (s *IntegrationTestSuite) Test_Liveness() {
	require := s.Require()
	err := WaitForNextBlock(s.cctx)
	require.NoError(err)
	// check that we're actually able to set the consensus params
	params, err := s.cctx.Client.ConsensusParams(context.TODO(), nil)
	require.NoError(err)
	require.Equal(int64(1), params.ConsensusParams.Block.TimeIotaMs)
	_, err = WaitForHeight(s.cctx, 20)
	require.NoError(err)
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}
