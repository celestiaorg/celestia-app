package testnode

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/rpc/core/types"
)

type IntegrationTestSuite struct {
	suite.Suite

	accounts []string
	cctx     Context
}

func (s *IntegrationTestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skipping full node integration test in short mode.")
	}

	s.T().Log("setting up integration test suite")

	s.accounts, s.cctx = DefaultNetwork(s.T(), 400*time.Millisecond)
}

func (s *IntegrationTestSuite) Test_Liveness() {
	require := s.Require()
	err := s.cctx.WaitForNextBlock()
	require.NoError(err)
	// check that we're actually able to set the consensus params
	var params *coretypes.ResultConsensusParams
	// this query can be flaky with fast block times, so we repeat it multiple
	// times in attempt to increase the probability of it working
	for i := 0; i < 20; i++ {
		params, err = s.cctx.Client.ConsensusParams(context.TODO(), nil)
		if err != nil || params == nil {
			continue
		}
		break
	}
	require.NotNil(params)
	require.Equal(int64(1), params.ConsensusParams.Block.TimeIotaMs)
	_, err = s.cctx.WaitForHeight(40)
	require.NoError(err)
}

func (s *IntegrationTestSuite) Test_PostData() {
	require := s.Require()
	_, err := s.cctx.PostData(s.accounts[0], flags.BroadcastBlock, namespace.RandomBlobNamespace(), tmrand.Bytes(100000))
	require.NoError(err)
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

func (s *IntegrationTestSuite) Test_FillBlock() {
	require := s.Require()

	for squareSize := 2; squareSize < appconsts.DefaultMaxSquareSize; squareSize *= 2 {
		resp, err := s.cctx.FillBlock(squareSize, s.accounts, flags.BroadcastAsync)
		require.NoError(err)

		err = s.cctx.WaitForNextBlock()
		require.NoError(err)

		res, err := testfactory.QueryWithoutProof(s.cctx.Context, resp.TxHash)
		require.NoError(err)
		require.Equal(abci.CodeTypeOK, res.TxResult.Code)

		b, err := s.cctx.Client.Block(context.TODO(), &res.Height)
		require.NoError(err)
		require.Equal(uint64(squareSize), b.Block.SquareSize)
	}
}

func (s *IntegrationTestSuite) Test_FillBlock_InvalidSquareSizeError() {
	tests := []struct {
		name        string
		squareSize  int
		expectedErr error
	}{
		{
			name:        "when squareSize less than 2",
			squareSize:  0,
			expectedErr: fmt.Errorf("unsupported squareSize: 0"),
		},
		{
			name:        "when squareSize is greater than 2 but not a power of 2",
			squareSize:  18,
			expectedErr: fmt.Errorf("unsupported squareSize: 18"),
		},
		{
			name:       "when squareSize is greater than 2 and a power of 2",
			squareSize: 16,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			_, actualErr := s.cctx.FillBlock(tc.squareSize, s.accounts, flags.BroadcastAsync)
			s.Equal(tc.expectedErr, actualErr)
		})
	}
}
