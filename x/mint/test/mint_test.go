package test

import (
	"context"
	"fmt"
	"testing"

	sdktypes "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc/metadata"

	"github.com/celestiaorg/celestia-app/v4/app/params"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
)

type IntegrationTestSuite struct {
	suite.Suite

	cctx testnode.Context
}

func TestMintIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping mint integration test in short mode.")
	}
	suite.Run(t, new(IntegrationTestSuite))
}

func (s *IntegrationTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up mint integration test suite")

	cparams := testnode.DefaultConsensusParams()
	cfg := testnode.DefaultConfig().WithConsensusParams(cparams)

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
}

// TestTotalSupplyIncreasesOverTime tests that the total supply of tokens
// increases over time as new blocks are added to the chain.
func (s *IntegrationTestSuite) TestTotalSupplyIncreasesOverTime() {
	require := s.Require()

	initialHeight := int64(1)
	laterHeight := int64(20)

	err := s.cctx.WaitForNextBlock()
	require.NoError(err)

	initialSupply := s.getTotalSupply(initialHeight)

	_, err = s.cctx.WaitForHeight(laterHeight + 1)
	require.NoError(err)
	laterSupply := s.getTotalSupply(laterHeight)

	require.True(initialSupply.AmountOf(params.BondDenom).LT(laterSupply.AmountOf(params.BondDenom)))
}

func (s *IntegrationTestSuite) getTotalSupply(height int64) sdktypes.Coins {
	require := s.Require()

	bqc := banktypes.NewQueryClient(s.cctx.GRPCClient)
	ctx := metadata.AppendToOutgoingContext(context.Background(), grpctypes.GRPCBlockHeightHeader, fmt.Sprintf("%d", height))

	resp, err := bqc.TotalSupply(ctx, &banktypes.QueryTotalSupplyRequest{})
	require.NoError(err)

	return resp.Supply
}
