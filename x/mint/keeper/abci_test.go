package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/test/util"
	minttypes "github.com/celestiaorg/celestia-app/v4/x/mint/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	v1 "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			name: "inflation rate is 0.08 for year zero",
			ctx: ctx.WithBlockHeight(1).WithBlockHeader(v1.Header{
				Time: *genesisTime,
			}),
			want: math.LegacyMustNewDecFromStr("0.08"),
		},
		{
			name: "inflation rate is 0.08 for year one minus one second",
			ctx:  ctx.WithBlockHeader(v1.Header{Time: yearOneMinusOneSecond}),
			want: math.LegacyMustNewDecFromStr("0.08"),
		},
		{
			name: "inflation rate is 0.072 for year one",
			ctx:  ctx.WithBlockHeader(v1.Header{Time: yearOne}),
			want: math.LegacyMustNewDecFromStr("0.072"),
		},
		{
			name: "inflation rate is 0.0648 for year two",
			ctx:  ctx.WithBlockHeader(v1.Header{Time: yearTwo}),
			want: math.LegacyMustNewDecFromStr("0.0648"),
		},
		{
			name: "inflation rate is 0.01647129056757192 for year fifteen",
			ctx:  ctx.WithBlockHeader(v1.Header{Time: yearFifteen}),
			want: math.LegacyMustNewDecFromStr("0.01647129056757192"),
		},
		{
			name: "inflation rate is 0.015 for year twenty",
			ctx:  ctx.WithBlockHeader(v1.Header{Time: yearTwenty}),
			want: math.LegacyMustNewDecFromStr("0.015"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			app.MintKeeper.BeginBlocker(tc.ctx)
			got, err := app.MintKeeper.InflationRate(ctx, &minttypes.QueryInflationRateRequest{})
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got.InflationRate)
		})
	}
}

func TestAnnualProvisions(t *testing.T) {
	t.Run("annual provisions are set when originally zero", func(t *testing.T) {
		a, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := sdk.NewContext(a.CommitMultiStore(), tmproto.Header{}, false, log.NewNopLogger())

		assert.True(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.IsZero())
		a.MintKeeper.BeginBlocker(ctx)
		assert.False(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.IsZero())
	})

	t.Run("annual provisions are not updated more than once per year", func(t *testing.T) {
		a, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := sdk.NewContext(a.CommitMultiStore(), tmproto.Header{}, false, log.NewNopLogger())
		genesisTime := a.MintKeeper.GetGenesisTime(ctx).GenesisTime
		yearOneMinusOneSecond := genesisTime.Add(oneYear).Add(-time.Second)

		initialSupply := math.NewInt(100_000_001_000_000)
		require.Equal(t, initialSupply, a.MintKeeper.StakingTokenSupply(ctx))

		bondDenom, err := a.StakingKeeper.BondDenom(ctx)
		require.NoError(t, err)
		require.Equal(t, a.MintKeeper.GetMinter(ctx).BondDenom, bondDenom)
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
				ctx = ctx.WithBlockHeight(tc.height).WithBlockHeader(v1.Header{Time: tc.time})
				a.MintKeeper.BeginBlocker(ctx)
				assert.True(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.Equal(want))
			})
		}

		t.Run("one year later", func(t *testing.T) {
			yearOne := genesisTime.Add(oneYear)
			ctx = ctx.WithBlockHeight(5).WithBlockHeader(v1.Header{Time: yearOne})
			a.MintKeeper.BeginBlocker(ctx)
			assert.False(t, a.MintKeeper.GetMinter(ctx).AnnualProvisions.Equal(want))
		})
	})
}
