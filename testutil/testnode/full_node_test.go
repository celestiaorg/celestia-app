package testnode

import (
	"context"
	"fmt"
	"testing"

	srvrconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/types"
)

type IntegrationTestSuite struct {
	suite.Suite

	tmNode *node.Node
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.T().Log("setting up integration test suite")

	var err error
	tmNode, _, err := NewTestNode(s.T(), config.DefaultConfig(), *srvrconfig.DefaultConfig(), true, "taco", "salad")
	require.NoError(s.T(), err)

	err = tmNode.Start()
	require.NoError(s.T(), err)
}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	s.tmNode.Stop()
}

func (s *IntegrationTestSuite) TestNetwork_Liveness() {
	// h, err := s.network.WaitForHeightWithTimeout(10, time.Minute)
	// s.Require().NoError(err, "expected to reach 10 blocks; got %d", h)
	sub, err := s.tmNode.EventBus().Subscribe(context.TODO(), "test", types.EventQueryNewBlock)
	require.NoError(s.T(), err)
	for msg := range sub.Out() {
		b, ok := msg.Data().(types.Block)
		if !ok {
			fmt.Println("not a block")
		}
		fmt.Println("found block", b.Height)
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}
