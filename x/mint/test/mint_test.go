package test

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/suite"
)

type IntegrationTestSuite struct {
	suite.Suite

	accounts []string
	cctx     testnode.Context
}

func (s *IntegrationTestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skipping mint integration test in short mode.")
	}

	s.T().Log("setting up mint integration test suite")
	s.accounts, s.cctx = testnode.DefaultNetwork(s.T())
}

// TestTotalSupplyIncreasesOverTime tests that the total supply of tokens
// increases over time as new blocks are added to the chain.
func (s *IntegrationTestSuite) TestTotalSupplyIncreasesOverTime() {
	require := s.Require()

	err := s.cctx.WaitForNextBlock()
	require.NoError(err)
	initalSupply := s.GetTotalSupply()

	_, err = s.cctx.WaitForHeight(20)
	require.NoError(err)
	laterSupply := s.GetTotalSupply()

	require.True(initalSupply.AmountOf(app.BondDenom).LT(laterSupply.AmountOf(app.BondDenom)))
}

func (s *IntegrationTestSuite) GetTotalSupply() sdktypes.Coins {
	require := s.Require()

	// Note: we can also query for total supply via
	// r1, r2, err := s.cctx.QueryWithData("/cosmos.bank.v1beta1.Query/TotalSupply", nil)
	res, err := s.cctx.Client.ABCIQuery(
		context.Background(),
		"/cosmos.bank.v1beta1.Query/TotalSupply",
		nil,
	)
	require.NoError(err)

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	var txResp banktypes.QueryTotalSupplyResponse
	require.NoError(cdc.Unmarshal(res.Response.Value, &txResp))

	return txResp.GetSupply()
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}
