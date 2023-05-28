package test

import (
	"context"
	"fmt"
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
	tenDays := oneDay * 10
	// Set the minimum time between blocks to 10 days. This will make the
	// timestamps between blocks increase by 10 days each block despite that
	// much time not actually passing. We do this to test the inflation rate
	// over time without having to wait one year for the test to complete.
	//
	// Example:
	// height 7 time 2023-07-18 02:04:19.091578814 +0000 UTC
	// height 8 time 2023-07-28 02:04:19.091578814 +0000 UTC
	// height 9 time 2023-08-07 02:04:19.091578814 +0000 UTC
	cparams.Block.TimeIotaMs = int64(tenDays.Milliseconds())

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

	initalSupply := s.GetTotalSupply(initialHeight)

	_, err = s.cctx.WaitForHeight(laterHeight)
	require.NoError(err)
	laterSupply := s.GetTotalSupply(laterHeight)

	require.True(initalSupply.AmountOf(app.BondDenom).LT(laterSupply.AmountOf(app.BondDenom)))
}

// TestInitialInflationRate tests that the initial inflation rate is
// approximately 8% per year.
func (s *IntegrationTestSuite) TestInitialInflationRate() {
	require := s.Require()
	oneYear := time.Duration(int64(minttypes.NanosecondsPerYear))

	err := s.cctx.WaitForNextBlock()
	require.NoError(err)
	initialSupply, initialTime := s.GetTotalSupplyAndTimestamp()

	_, err = s.cctx.WaitForHeight(10)
	require.NoError(err)
	laterSupply, laterTime := s.GetTotalSupplyAndTimestamp()

	diffSupply := laterSupply.AmountOf(app.BondDenom).Sub(initialSupply.AmountOf(app.BondDenom))
	diffTime := laterTime.Sub(initialTime)

	projectedAnnualProvisions := diffSupply.Mul(sdktypes.NewInt(oneYear.Nanoseconds())).Quo(sdktypes.NewInt(diffTime.Nanoseconds()))
	initialInflationRate := minttypes.InitialInflationRateAsDec()
	expectedAnnualProvisions := initialInflationRate.Mul(sdktypes.NewDecFromBigInt(initialSupply.AmountOf(app.BondDenom).BigInt())).TruncateInt()
	diffAnnualProvisions := projectedAnnualProvisions.Sub(expectedAnnualProvisions).Abs()

	// Note we use a .01 margin of error because the projected annual provisions
	// are based on a small block time iwindow.
	marginOfError := sdktypes.NewDecWithPrec(1, 2) // .01
	actualError := sdktypes.NewDecFromBigInt(diffAnnualProvisions.BigInt()).Quo(sdktypes.NewDecFromBigInt(initialSupply.AmountOf(app.BondDenom).BigInt()))

	require.True(actualError.LTE(marginOfError))
}

// TestInflationRate tests that the inflation rate matches the expected rate per year.
//
// | Year | Inflation (%)     |
// |------|-------------------|
// | 0    | 8.00              |
// | 1    | 7.20              |
// | 2    | 6.48              |
// | 3    | 5.832             |
// | 4    | 5.2488            |
// | 5    | 4.72392           |
// | 6    | 4.251528          |
// | 7    | 3.8263752         |
// | 8    | 3.44373768        |
// | 9    | 3.099363912       |
// | 10   | 2.7894275208      |
// | 11   | 2.51048476872     |
// | 12   | 2.259436291848    |
// | 13   | 2.0334926626632   |
// | 14   | 1.83014339639688  |
// | 15   | 1.647129056757192 |
// | 16   | 1.50              |
// | 17   | 1.50              |
// | 18   | 1.50              |
// | 19   | 1.50              |
// | 20   | 1.50              |
func (s *IntegrationTestSuite) TestInflationRate() {
	require := s.Require()

	type testCase struct {
		year int
		want sdktypes.Dec
	}

	testCases := []testCase{
		{year: 0, want: sdktypes.MustNewDecFromStr("8.00")},
		{year: 1, want: sdktypes.MustNewDecFromStr("7.20")},
	}
	genesisTime, err := s.cctx.GenesisTime()
	fmt.Printf("genesisTime: %v\n", genesisTime)
	require.NoError(err)

	for _, tc := range testCases {
		wantTimestamp := genesisTime.Add(time.Duration((tc.year + 1) * minttypes.NanosecondsPerYear))
		fmt.Printf("wantTimestamp: %v\n", wantTimestamp)

		initialSupply, initialTime := s.GetTotalSupplyAndTimestamp()
		fmt.Printf("initial supply: %v and time %v \n", initialSupply, initialTime)

		wantAsFloat, err := tc.want.Float64()
		require.NoError(err)
		fmt.Printf("wantAsFloat: %v\n", wantAsFloat)

		_, err = s.cctx.WaitForTimestampWithTimeout(wantTimestamp, 20*time.Second)
		require.NoError(err)

		laterSupply, laterTime := s.GetTotalSupplyAndTimestamp()
		fmt.Printf("later supply: %v and timestamp %v\n", laterSupply, laterTime)

		estimateInflationRate(initialSupply, initialTime, laterSupply, laterTime)
	}
}

func (s *IntegrationTestSuite) GetTotalSupply(height int64) sdktypes.Coins {
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

func (s *IntegrationTestSuite) GetTimestamp(height int64) time.Time {
	require := s.Require()

	block, err := s.cctx.WithHeight(height).Client.Block(context.Background(), &height)
	require.NoError(err)
	return block.Block.Header.Time
}

func (s *IntegrationTestSuite) GetTotalSupplyAndTimestamp() (sdktypes.Coins, time.Time) {
	require := s.Require()

	info, err := s.cctx.Client.ABCIInfo(context.Background())
	require.NoError(err)
	height := info.Response.LastBlockHeight

	totalSupply := s.GetTotalSupply(height)
	timestamp := s.GetTimestamp(height)
	return totalSupply, timestamp
}

func estimateInflationRate(initialSupply sdktypes.Coins, initialTimestamp time.Time, laterSupply sdktypes.Coins, laterTimestamp time.Time) sdktypes.Dec {
	// oneYear := time.Duration(int64(minttypes.NanosecondsPerYear))

	// diffSupply := laterSupply.AmountOf(app.BondDenom).Sub(initialSupply.AmountOf(app.BondDenom))
	// diffTime := laterTimestamp.Sub(initialTimestamp)

	// projectedAnnualProvisions := diffSupply.Mul(sdktypes.NewInt(oneYear.Nanoseconds())).Quo(sdktypes.NewInt(diffTime.Nanoseconds()))

	// laterSupply = initalSupply + (initialSupply * inflationRate)

	inflationRate := sdktypes.NewDecFromBigInt(laterSupply.AmountOf(app.BondDenom).Sub(initialSupply.AmountOf(app.BondDenom)).BigInt()).QuoInt(initialSupply.AmountOf(app.BondDenom))
	fmt.Printf("inflationRate %v\n", inflationRate.String())
	return inflationRate
	// inflationRate := projectedAnnualProvisions.ToDec().Quo(sdktypes.NewDecFromInt(initialSupply.AmountOf(app.BondDenom)))

	// initialInflationRate := minttypes.InitialInflationRateAsDec()
	// expectedAnnualProvisions := initialInflationRate.Mul(sdktypes.NewDecFromBigInt(initialSupply.AmountOf(app.BondDenom).BigInt())).TruncateInt()
	// diffAnnualProvisions := projectedAnnualProvisions.Sub(expectedAnnualProvisions).Abs()

	// Note we use a .01 margin of error because the projected annual provisions
	// are based on a small block time iwindow.
	// marginOfError := sdktypes.NewDecWithPrec(1, 2) // .01
	// actualError := sdktypes.NewDecFromBigInt(diffAnnualProvisions.BigInt()).Quo(sdktypes.NewDecFromBigInt(initialSupply.AmountOf(app.BondDenom).BigInt()))
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}
