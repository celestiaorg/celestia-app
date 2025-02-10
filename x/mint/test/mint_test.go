package test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	minttypes "github.com/celestiaorg/celestia-app/v3/x/mint/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc/metadata"
)

type IntegrationTestSuite struct {
	suite.Suite

	cctx testnode.Context
}

func (s *IntegrationTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up mint integration test suite")

	cparams := testnode.DefaultConsensusParams()
	oneDay := time.Hour * 24
	oneMonth := oneDay * 30
	sixMonths := oneMonth * 6
	// Set the minimum time between blocks to six months. This will make the
	// timestamps between blocks increase by six months each block despite that
	// much time not actually passing. We do this to test the inflation rate
	// over time without having to wait one year for the test to complete.
	//
	// Note: if TimeIotaMs is removed from CometBFT, this technique will no
	// longer work.
	cparams.Block.TimeIotaMs = sixMonths.Milliseconds()

	cfg := testnode.DefaultConfig().
		WithConsensusParams(cparams)

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

	require.True(initialSupply.AmountOf(app.BondDenom).LT(laterSupply.AmountOf(app.BondDenom)))
}

// TestInflationRate verifies that the inflation rate each year matches the
// expected rate of inflation. See the README.md for the expected rate of
// inflation.
func (s *IntegrationTestSuite) TestInflationRate() {
	require := s.Require()

	type testCase struct {
		year int64
		want sdktypes.Dec
	}
	testCases := []testCase{
		{year: 0, want: sdktypes.MustNewDecFromStr("8.00")},
		{year: 1, want: sdktypes.MustNewDecFromStr("7.20")},
		{year: 2, want: sdktypes.MustNewDecFromStr("6.48")},
		{year: 3, want: sdktypes.MustNewDecFromStr("5.832")},
		{year: 4, want: sdktypes.MustNewDecFromStr("5.2488")},
		{year: 5, want: sdktypes.MustNewDecFromStr("4.72392")},
		// Note: since the testnode takes time to create blocks, test cases
		// for years 6+ will time out give the current TimeIotaMs.
	}

	genesisTime, err := s.cctx.GenesisTime()
	require.NoError(err)

	lastYear := testCases[len(testCases)-1].year
	lastTimestamp := genesisTime.Add(time.Duration((lastYear + 1) * minttypes.NanosecondsPerYear))
	_, err = s.cctx.WaitForTimestamp(lastTimestamp)
	require.NoError(err)

	for _, tc := range testCases {
		startTimestamp := genesisTime.Add(time.Duration(tc.year * minttypes.NanosecondsPerYear))
		endTimestamp := genesisTime.Add(time.Duration((tc.year + 1) * minttypes.NanosecondsPerYear))

		startHeight, err := s.cctx.HeightForTimestamp(startTimestamp)
		require.NoError(err)
		endHeight, err := s.cctx.HeightForTimestamp(endTimestamp)
		require.NoError(err)

		inflationRate := s.estimateInflationRate(startHeight, endHeight)
		actualError := inflationRate.Sub(tc.want).Abs()
		marginOfError := sdktypes.MustNewDecFromStr("0.01")
		if marginOfError.GT(actualError) {
			s.Failf("TestInflationRate failure", "inflation rate for year %v is %v, want %v with a .01 margin of error", tc.year, inflationRate, tc.want)
		}
	}
}

func (s *IntegrationTestSuite) getTotalSupply(height int64) sdktypes.Coins {
	require := s.Require()

	bqc := banktypes.NewQueryClient(s.cctx.GRPCClient)
	ctx := metadata.AppendToOutgoingContext(context.Background(), grpctypes.GRPCBlockHeightHeader, fmt.Sprintf("%d", height))

	resp, err := bqc.TotalSupply(ctx, &banktypes.QueryTotalSupplyRequest{})
	require.NoError(err)

	return resp.Supply
}

func (s *IntegrationTestSuite) estimateInflationRate(startHeight int64, endHeight int64) sdktypes.Dec {
	startSupply := s.getTotalSupply(startHeight).AmountOf(app.BondDenom)
	endSupply := s.getTotalSupply(endHeight).AmountOf(app.BondDenom)
	diffSupply := endSupply.Sub(startSupply)

	return sdktypes.NewDecFromBigInt(diffSupply.BigInt()).QuoInt(startSupply)
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestMintIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping mint integration test in short mode.")
	}
	suite.Run(t, new(IntegrationTestSuite))
}
