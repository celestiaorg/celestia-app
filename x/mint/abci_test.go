package mint_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/mint"
	minttypes "github.com/celestiaorg/celestia-app/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestGenesisTime(t *testing.T) {
	app, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())
	unixEpoch := time.Unix(0, 0).UTC()
	fixedTime := time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()

	type testCase struct {
		name string
		ctx  sdk.Context
		want time.Time
	}

	testCases := []testCase{
		{
			name: "initially genesis time is unix epoch",
			ctx:  ctx.WithBlockHeight(0).WithBlockTime(unixEpoch),
			want: unixEpoch,
		},
		{
			name: "genesis time is set to time of first block",
			ctx:  ctx.WithBlockHeight(1).WithBlockTime(fixedTime),
			want: fixedTime,
		},
		{
			name: "genesis time remains set to time of first block",
			ctx:  ctx.WithBlockHeight(2).WithBlockTime(fixedTime.Add(time.Hour)),
			want: fixedTime,
		},
	}

	for _, tc := range testCases {
		mint.BeginBlocker(tc.ctx, app.MintKeeper)
		got, err := app.MintKeeper.GenesisTime(ctx, &minttypes.QueryGenesisTimeRequest{})
		assert.NoError(t, err)
		assert.Equal(t, &tc.want, got.GenesisTime)
	}
}

func TestInflationRate(t *testing.T) {
	app, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())
	unixEpoch := time.Unix(0, 0).UTC()
	yearZero := time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	oneYear := time.Duration(minttypes.NanosecondsPerYear)
	yearOne := yearZero.Add(oneYear)
	yearTwo := yearZero.Add(2 * oneYear)
	yearTwenty := yearZero.Add(20 * oneYear)

	type testCase struct {
		name string
		ctx  sdk.Context
		want sdk.Dec
	}

	testCases := []testCase{
		{
			name: "inflation rate is 0.08 initially",
			ctx:  ctx.WithBlockHeight(0).WithBlockTime(unixEpoch),
			want: sdk.NewDecWithPrec(8, 2), // 0.08
		},
		{
			name: "inflation rate is 0.08 for year zero",
			ctx:  ctx.WithBlockHeight(1).WithBlockTime(yearZero),
			want: sdk.NewDecWithPrec(8, 2), // 0.08
		},
		{
			name: "inflation rate is 0.08 for year one minus one minute",
			ctx:  ctx.WithBlockTime(yearOne.Add(-time.Minute)),
			want: sdk.NewDecWithPrec(8, 2), // 0.08
		},
		{
			name: "inflation rate is 0.072 for year one",
			ctx:  ctx.WithBlockTime(yearOne),
			want: sdk.NewDecWithPrec(72, 3), // 0.072
		},
		{
			name: "inflation rate is 0.0648 for year two",
			ctx:  ctx.WithBlockTime(yearTwo),
			want: sdk.NewDecWithPrec(648, 4), // 0.0648
		},
		{
			name: "inflation rate is 0.015 for year twenty",
			ctx:  ctx.WithBlockTime(yearTwenty),
			want: sdk.NewDecWithPrec(15, 3), // 0.015
		},
	}

	for _, tc := range testCases {
		mint.BeginBlocker(tc.ctx, app.MintKeeper)
		got, err := app.MintKeeper.InflationRate(ctx, &minttypes.QueryInflationRateRequest{})
		assert.NoError(t, err)
		assert.Equal(t, tc.want, got.InflationRate)
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

		initialSupply := sdk.NewInt(100_000_001_000_000)
		require.Equal(t, initialSupply, a.MintKeeper.StakingTokenSupply(ctx))
		require.Equal(t, a.MintKeeper.GetMinter(ctx).BondDenom, a.StakingKeeper.BondDenom(ctx))
		require.True(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.IsZero())

		blockInterval := time.Second * 15
		firstBlockTime := time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
		oneYear := time.Duration(minttypes.NanosecondsPerYear)
		lastBlockInYear := firstBlockTime.Add(oneYear).Add(-time.Second)

		want := minttypes.InitialInflationRateAsDec().MulInt(initialSupply)

		type testCase struct {
			height int64
			time   time.Time
		}
		testCases := []testCase{
			{1, firstBlockTime},
			{2, firstBlockTime.Add(blockInterval)},
			{3, firstBlockTime.Add(blockInterval * 2)},
			{4, lastBlockInYear},
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
			ctx = ctx.WithBlockHeight(5).WithBlockTime(lastBlockInYear.Add(time.Second))
			mint.BeginBlocker(ctx, a.MintKeeper)
			assert.False(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.Equal(want))
		})
	})
}
