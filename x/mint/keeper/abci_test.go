package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/test/util"
	minttypes "github.com/celestiaorg/celestia-app/v4/x/mint/types"
)

var oneYear = time.Duration(minttypes.NanosecondsPerYear)

func TestInflationRate(t *testing.T) {
	app, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := sdk.NewContext(app.CommitMultiStore(), tmproto.Header{}, false, log.NewNopLogger())
	genesisTime := app.MintKeeper.GetGenesisTime(ctx).GenesisTime

	yearOneMinusOneSecond := genesisTime.Add(oneYear).Add(-time.Second)
	yearOne := genesisTime.Add(oneYear)
	yearTwo := genesisTime.Add(2 * oneYear)
	yearFifteen := genesisTime.Add(15 * oneYear)
	yearTwenty := genesisTime.Add(20 * oneYear)

	type testCase struct {
		name string
		ctx  sdk.Context
		want math.LegacyDec
	}

	testCases := []testCase{
		{
			name: "inflation rate is 0.0536 for year zero",
			ctx:  ctx.WithBlockTime(*genesisTime),
			want: math.LegacyMustNewDecFromStr("0.0536"),
		},
		{
			name: "inflation rate is 0.0536 for year one minus one second",
			ctx:  ctx.WithBlockTime(yearOneMinusOneSecond),
			want: math.LegacyMustNewDecFromStr("0.0536"),
		},
		{
			name: "inflation rate is 0.0500088 for year one",
			ctx:  ctx.WithBlockTime(yearOne),
			want: math.LegacyMustNewDecFromStr("0.0500088"),
		},
		{
			name: "inflation rate is 0.0466582104 for year two",
			ctx:  ctx.WithBlockTime(yearTwo),
			want: math.LegacyMustNewDecFromStr("0.0466582104"),
		},
		{
			name: "inflation rate is 0.018940413053647755 for year fifteen",
			ctx:  ctx.WithBlockTime(yearFifteen),
			want: math.LegacyMustNewDecFromStr("0.018940413053647755"),
		},
		{
			name: "inflation rate is 0.015 for year twenty",
			ctx:  ctx.WithBlockTime(yearTwenty),
			want: math.LegacyMustNewDecFromStr("0.015"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := app.MintKeeper.BeginBlocker(tc.ctx)
			assert.NoError(t, err)
			got, err := app.MintKeeper.InflationRate(tc.ctx, &minttypes.QueryInflationRateRequest{})
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got.InflationRate)
		})
	}
}

func TestAnnualProvisions(t *testing.T) {
	t.Run("annual provisions are set when originally zero", func(t *testing.T) {
		a, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := sdk.NewContext(a.CommitMultiStore(), tmproto.Header{}, false, log.NewNopLogger())
		genesisTime := a.MintKeeper.GetGenesisTime(ctx).GenesisTime
		ctx = ctx.WithBlockTime(*genesisTime)

		// note, the is 0 case, isn't tested here, as the first block is committed during test app setup

		assert.False(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.IsZero(), fmt.Sprintf("is %s", a.MintKeeper.GetMinter(ctx).AnnualProvisions))
	})

	t.Run("annual provisions are not updated more than once per year", func(t *testing.T) {
		a, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := sdk.NewContext(a.CommitMultiStore(), tmproto.Header{}, false, log.NewNopLogger())
		genesisTime := a.MintKeeper.GetGenesisTime(ctx).GenesisTime
		ctx = ctx.WithBlockTime(*genesisTime)

		yearOneMinusOneSecond := genesisTime.Add(oneYear).Add(-time.Second)

		initialSupply := math.NewInt(100_000_001_000_000)
		require.Equal(t, initialSupply, a.MintKeeper.StakingTokenSupply(ctx))

		bondDenom, err := a.StakingKeeper.BondDenom(ctx)
		require.NoError(t, err)
		require.Equal(t, a.MintKeeper.GetMinter(ctx).BondDenom, bondDenom)

		blockInterval := time.Second * 15

		want := minttypes.InitialInflationRateAsDec().MulInt(initialSupply)

		type testCase struct {
			height int64
			time   time.Time
		}
		testCases := []testCase{
			{1, genesisTime.Add(blockInterval)},
			{2, genesisTime.Add(blockInterval * 2)},
			{3, yearOneMinusOneSecond},
			// testing annual provisions for years after year zero depends on the
			// total supply which increased due to inflation in year zero.
		}

		for _, tc := range testCases {
			t.Run(fmt.Sprintf("block height %v", tc.height), func(t *testing.T) {
				ctx = ctx.WithBlockHeight(tc.height).WithBlockTime(tc.time)
				assert.NoError(t, a.MintKeeper.BeginBlocker(ctx))
				assert.True(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.Equal(want))
			})
		}

		t.Run("one year later", func(t *testing.T) {
			yearOne := genesisTime.Add(oneYear)
			ctx = ctx.WithBlockHeight(5).WithBlockTime(yearOne)
			assert.NoError(t, a.MintKeeper.BeginBlocker(ctx))
			assert.False(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.Equal(want))
		})
	})
}
