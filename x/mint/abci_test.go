package mint_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/x/mint"
	minttypes "github.com/celestiaorg/celestia-app/v2/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

var oneYear = time.Duration(minttypes.NanosecondsPerYear)

func TestInflationRate(t *testing.T) {
	app, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())
	genesisTime := app.MintKeeper.GetGenesisTime(ctx).GenesisTime

	yearOneMinusOneSecond := genesisTime.Add(oneYear).Add(-time.Second)
	yearOne := genesisTime.Add(oneYear)
	yearTwo := genesisTime.Add(2 * oneYear)
	yearFifteen := genesisTime.Add(15 * oneYear)
	yearTwenty := genesisTime.Add(20 * oneYear)

	type testCase struct {
		name string
		ctx  sdk.Context
		want sdk.Dec
	}

	testCases := []testCase{
		{
			name: "inflation rate is 0.08 for year zero",
			ctx:  ctx.WithBlockHeight(1).WithBlockTime(*genesisTime),
			want: sdk.MustNewDecFromStr("0.08"),
		},
		{
			name: "inflation rate is 0.08 for year one minus one second",
			ctx:  ctx.WithBlockTime(yearOneMinusOneSecond),
			want: sdk.MustNewDecFromStr("0.08"),
		},
		{
			name: "inflation rate is 0.072 for year one",
			ctx:  ctx.WithBlockTime(yearOne),
			want: sdk.MustNewDecFromStr("0.072"),
		},
		{
			name: "inflation rate is 0.0648 for year two",
			ctx:  ctx.WithBlockTime(yearTwo),
			want: sdk.MustNewDecFromStr("0.0648"),
		},
		{
			name: "inflation rate is 0.01647129056757192 for year fifteen",
			ctx:  ctx.WithBlockTime(yearFifteen),
			want: sdk.MustNewDecFromStr("0.01647129056757192"),
		},
		{
			name: "inflation rate is 0.015 for year twenty",
			ctx:  ctx.WithBlockTime(yearTwenty),
			want: sdk.MustNewDecFromStr("0.015"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mint.BeginBlocker(tc.ctx, app.MintKeeper)
			got, err := app.MintKeeper.InflationRate(ctx, &minttypes.QueryInflationRateRequest{})
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got.InflationRate)
		})
	}
}

func TestAnnualProvisions(t *testing.T) {
	t.Run("annual provisions are set when originally zero", func(t *testing.T) {
		a, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := sdk.NewContext(a.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())

		assert.True(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.IsZero())
		mint.BeginBlocker(ctx, a.MintKeeper)
		assert.False(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.IsZero())
	})

	t.Run("annual provisions are not updated more than once per year", func(t *testing.T) {
		a, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := sdk.NewContext(a.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())
		genesisTime := a.MintKeeper.GetGenesisTime(ctx).GenesisTime
		yearOneMinusOneSecond := genesisTime.Add(oneYear).Add(-time.Second)

		initialSupply := sdk.NewInt(100_000_001_000_000)
		require.Equal(t, initialSupply, a.MintKeeper.StakingTokenSupply(ctx))
		require.Equal(t, a.MintKeeper.GetMinter(ctx).BondDenom, a.StakingKeeper.BondDenom(ctx))
		require.True(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.IsZero())

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
				mint.BeginBlocker(ctx, a.MintKeeper)
				assert.True(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.Equal(want))
			})
		}

		t.Run("one year later", func(t *testing.T) {
			yearOne := genesisTime.Add(oneYear)
			ctx = ctx.WithBlockHeight(5).WithBlockTime(yearOne)
			mint.BeginBlocker(ctx, a.MintKeeper)
			assert.False(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.Equal(want))
		})
	})
}
