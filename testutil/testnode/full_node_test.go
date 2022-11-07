package testnode

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/rpc/core/types"
)

type IntegrationTestSuite struct {
	suite.Suite

	cleanups []func()
	accounts []string
	cctx     Context
}

func (s *IntegrationTestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skipping test in unit-tests or race-detector mode.")
	}

	s.T().Log("setting up integration test suite")
	require := s.Require()

	// we create an arbitrary number of funded accounts
	for i := 0; i < 300; i++ {
		s.accounts = append(s.accounts, tmrand.Str(9))
	}

	tmNode, app, cctx, err := New(s.T(), DefaultParams(), DefaultTendermintConfig(), false, s.accounts...)
	require.NoError(err)

	cctx, stopNode, err := StartNode(tmNode, cctx)
	require.NoError(err)
	s.cleanups = append(s.cleanups, stopNode)

	cctx, cleanupGRPC, err := StartGRPCServer(app, DefaultAppConfig(), cctx)
	require.NoError(err)
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
	err := s.cctx.WaitForNextBlock()
	require.NoError(err)
	// check that we're actually able to set the consensus params
	var params *coretypes.ResultConsensusParams
	// this query can be flaky with fast block times, so we repeat it multiple
	// times in attempt to increase the probability of it working
	for i := 0; i < 10; i++ {
		params, err = s.cctx.Client.ConsensusParams(context.TODO(), nil)
		if err != nil || params == nil {
			continue
		}
		break
	}
	require.NotNil(params)
	require.Equal(int64(1), params.ConsensusParams.Block.TimeIotaMs)
	_, err = s.cctx.WaitForHeight(20)
	require.NoError(err)
}

func (s *IntegrationTestSuite) Test_PostData() {
	require := s.Require()
	_, err := s.cctx.PostData(s.accounts[0], flags.BroadcastBlock, namespace.RandomMessageNamespace(), tmrand.Bytes(100000))
	require.NoError(err)
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

func (s *IntegrationTestSuite) Test_FillBlock() {
	require := s.Require()

	for squareSize := 2; squareSize < appconsts.MaxSquareSize; squareSize *= 2 {
		resp, err := s.cctx.FillBlock(squareSize, s.accounts, flags.BroadcastAsync)
		require.NoError(err)

		err = s.cctx.WaitForNextBlock()
		require.NoError(err)

		res, err := testutil.QueryWithoutProof(s.cctx.Context, resp.TxHash)
		require.NoError(err)
		require.Equal(abci.CodeTypeOK, res.TxResult.Code)

		b, err := s.cctx.Client.Block(context.TODO(), &res.Height)
		require.NoError(err)
		require.Equal(uint64(squareSize), b.Block.OriginalSquareSize)
	}
}
