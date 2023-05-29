package test

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	minttypes "github.com/celestiaorg/celestia-app/x/mint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/rpc/client"
)

type IntegrationTestSuite struct {
	suite.Suite

	cctx testnode.Context
}

func (s *IntegrationTestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skipping mint integration test in short mode.")
	}

	t := s.T()
	t.Log("setting up mint integration test suite")

	cparams := testnode.DefaultParams()
	oneDay := time.Hour * 24
	oneMonth := oneDay * 30
	// Set the minimum time between blocks to 10 days. This will make the
	// timestamps between blocks increase by 10 days each block despite that
	// much time not actually passing. We do this to test the inflation rate
	// over time without having to wait one year for the test to complete.
	//
	// Example:
	// height 7 time 2023-07-18 02:04:19.091578814 +0000 UTC
	// height 8 time 2023-08-18 02:04:19.091578814 +0000 UTC
	// height 9 time 2023-09-18 02:04:19.091578814 +0000 UTC
	cparams.Block.TimeIotaMs = int64(oneMonth.Milliseconds())

	cctx, _, _ := testnode.NewNetwork(t, cparams, testnode.DefaultTendermintConfig(), testnode.DefaultAppConfig(), []string{})
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

	initalSupply := s.getTotalSupply(initialHeight)

	_, err = s.cctx.WaitForHeight(laterHeight)
	require.NoError(err)
	laterSupply := s.getTotalSupply(laterHeight)

	require.True(initalSupply.AmountOf(app.BondDenom).LT(laterSupply.AmountOf(app.BondDenom)))
}

// TestInflationRate verifies that the inflation rate each year matches the
// expected rate of inflation. See the README.md for the expected rate of
// inflation.
func (s *IntegrationTestSuite) TestInflationRate() {
	require := s.Require()

	type testCase struct {
		year int
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
	_, err = s.cctx.WaitForTimestampWithTimeout(lastTimestamp, 20*time.Second)
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

	options := &client.ABCIQueryOptions{Height: height}
	result, err := s.cctx.Client.ABCIQueryWithOptions(
		context.Background(),
		"/cosmos.bank.v1beta1.Query/TotalSupply",
		nil,
		*options,
	)
	require.NoError(err)

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	var txResp banktypes.QueryTotalSupplyResponse
	require.NoError(cdc.Unmarshal(result.Response.Value, &txResp))
	return txResp.GetSupply()
}

func (s *IntegrationTestSuite) estimateInflationRate(startHeight int64, endHeight int64) sdktypes.Dec {
	startSupply := s.getTotalSupply(startHeight).AmountOf(app.BondDenom)
	endSupply := s.getTotalSupply(endHeight).AmountOf(app.BondDenom)
	diffSupply := endSupply.Sub(startSupply)

	return sdktypes.NewDecFromBigInt(diffSupply.BigInt()).QuoInt(startSupply)
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}
