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
	sixMonths := time.Duration(6 * 30 * 24 * time.Hour) // 6 months * 30 days * 24 hours
	yearZeroAndSixMonths := yearZero.Add(sixMonths)
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
