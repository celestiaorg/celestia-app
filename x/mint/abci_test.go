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
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestGenesisTime(t *testing.T) {
	unixEpoch := time.Unix(0, 0).UTC()
	genesisTime := time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()

	app, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), genesisTime)
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())

	type testCase struct {
		name string
		ctx  sdk.Context
		want time.Time
	}

	testCases := []testCase{
		{
			name: "genesis time is set for block 0",
			ctx:  ctx.WithBlockHeight(0).WithBlockTime(unixEpoch),
			want: genesisTime,
		},
		{
			name: "genesis time is set for block 1",
			ctx:  ctx.WithBlockHeight(1).WithBlockTime(genesisTime),
			want: genesisTime,
		},
		{
			name: "genesis time remains set for future block heights and block times",
			ctx:  ctx.WithBlockHeight(2).WithBlockTime(genesisTime.Add(time.Hour)),
			want: genesisTime,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := app.MintKeeper.GenesisTime(ctx, &minttypes.QueryGenesisTimeRequest{})
			assert.NoError(t, err)
			assert.Equal(t, &tc.want, got.GenesisTime)
		})
	}
}

func TestInflationRate(t *testing.T) {
	genesisTime := time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	app, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), genesisTime)
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())
	unixEpoch := time.Unix(0, 0).UTC()
	oneYear, err := time.ParseDuration(fmt.Sprintf("%vns", minttypes.NanosecondsPerYear))
	assert.NoError(t, err)
	yearOne := genesisTime.Add(oneYear)
	yearTwo := genesisTime.Add(2 * oneYear)
	yearTwenty := genesisTime.Add(20 * oneYear)

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
			ctx:  ctx.WithBlockHeight(1).WithBlockTime(genesisTime),
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
	genesisTime := time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	app, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), genesisTime)
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())
	unixEpoch := time.Unix(0, 0).UTC()
	oneYear, err := time.ParseDuration(fmt.Sprintf("%vns", minttypes.NanosecondsPerYear))
	assert.NoError(t, err)
	yearOne := genesisTime.Add(oneYear)
	yearTwo := genesisTime.Add(2 * oneYear)
	yearTwenty := genesisTime.Add(20 * oneYear)

	type testCase struct {
		name string
		ctx  sdk.Context
		want sdk.Dec
	}

	testCases := []testCase{
		{
			name: "annual provisions is 80,000 initially",
			ctx:  ctx.WithBlockHeight(0).WithBlockTime(unixEpoch),
			want: sdk.NewDec(80_000), // 1,000,000 (total supply) * 0.08 (inflation rate)
		},
		{
			name: "annual provisions is 80,000 for year zero",
			ctx:  ctx.WithBlockHeight(1).WithBlockTime(genesisTime),
			want: sdk.NewDec(80_000), // 1,000,000 (total supply) * 0.08 (inflation rate)
		},
		{
			name: "annual provisions is 80,000 for year one minus one minute",
			ctx:  ctx.WithBlockTime(yearOne.Add(-time.Minute)),
			want: sdk.NewDec(80_000), // 1,000,000 (total supply) * 0.08 (inflation rate)
		},
		{
			name: "annual provisions is 72,000 for year one",
			ctx:  ctx.WithBlockTime(yearOne),
			want: sdk.NewDec(72_000), // 1,000,000 (total supply) * 0.072 (inflation rate)
		},
		{
			name: "annual provisions is 64,800 for year two",
			ctx:  ctx.WithBlockTime(yearTwo),
			want: sdk.NewDec(64_800), // 1,000,000 (total supply) * 0.0648 (inflation rate)
		},
		{
			name: "annual provisions is 15,000 for year twenty",
			ctx:  ctx.WithBlockTime(yearTwenty),
			want: sdk.NewDec(15_000), // 1,000,000 (total supply) * 0.015 (inflation rate)
		},
	}

	for _, tc := range testCases {
		mint.BeginBlocker(tc.ctx, app.MintKeeper)
		got, err := app.MintKeeper.AnnualProvisions(ctx, &minttypes.QueryAnnualProvisionsRequest{})
		assert.NoError(t, err)
		assert.Equal(t, tc.want, got.AnnualProvisions)
	}
}
