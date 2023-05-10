package mint_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/mint"
	minttypes "github.com/celestiaorg/celestia-app/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestGenesisTime(t *testing.T) {
	app, _ := util.SetupTestAppWithGenesisValSet()
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
	app, _ := util.SetupTestAppWithGenesisValSet()
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())
	unixEpoch := time.Unix(0, 0).UTC()
	yearZero := time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	yearZeroAndSixMonths := time.Date(2023, 6, 1, 1, 1, 1, 1, time.UTC).UTC()
	yearOne := time.Date(2024, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	yearTwo := time.Date(2025, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	yearTwenty := time.Date(2043, 1, 1, 1, 1, 1, 1, time.UTC).UTC()

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
			name: "inflation rate is 0.08 for year zero and six months",
			ctx:  ctx.WithBlockTime(yearZeroAndSixMonths),
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
	app, _ := util.SetupTestAppWithGenesisValSet()
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())
	unixEpoch := time.Unix(0, 0).UTC()
	yearZero := time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	yearZeroAndSixMonths := time.Date(2023, 6, 1, 1, 1, 1, 1, time.UTC).UTC()
	yearOne := time.Date(2024, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	yearTwo := time.Date(2025, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	yearTwenty := time.Date(2043, 1, 1, 1, 1, 1, 1, time.UTC).UTC()

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
			ctx:  ctx.WithBlockHeight(1).WithBlockTime(yearZero),
			want: sdk.NewDec(80_000), // 1,000,000 (total supply) * 0.08 (inflation rate)
		},
		{
			name: "annual provisions is 80,000 for year zero and six months",
			ctx:  ctx.WithBlockTime(yearZeroAndSixMonths),
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
		// Run BeginBlocker twice because minter uses the previous inflation
		// rate in minter to calculate the annual provisions. By running this
		// twice, we can test against block times rather than simulating a block
		// at yearOne and another at yearOne + 15 seconds.
		mint.BeginBlocker(tc.ctx, app.MintKeeper)
		mint.BeginBlocker(tc.ctx, app.MintKeeper)
		got, err := app.MintKeeper.AnnualProvisions(ctx, &minttypes.QueryAnnualProvisionsRequest{})
		assert.NoError(t, err)
		assert.Equal(t, tc.want, got.AnnualProvisions)
	}
}
