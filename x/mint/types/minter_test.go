package types

import (
	fmt "fmt"
	"math/rand"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app/params"
)

func TestCalculateInflationRate(t *testing.T) {
	minter := DefaultMinter()
	genesisTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	type testCase struct {
		year int64
		want float64
	}

	testCases := []testCase{
		{0, 0.0536}, // NOTE: this value won't be used in production because CIP-29 has been introduced after year 0 (see CIP-29 for details).
		{1, 0.0500088},
		{2, 0.0466582104},
		{3, 0.0435321103032},
		{4, 0.0406154589128856},
		{5, 0.03789422316572227},
		{6, 0.035355310213618873},
		{7, 0.03298650442930641},
		{8, 0.030776408632542877},
		{9, 0.028714389254162507},
		{10, 0.026790525174133616},
		{11, 0.024995559987466665},
		{12, 0.023320857468306398},
		{13, 0.02175836001792987},
		{14, 0.020300549896728567},
		{15, 0.018940413053647756},
		{16, 0.017671405379053356},
		{17, 0.016487421218656782},
		{18, 0.015382763997006776},
		{19, 0.015},
		{20, 0.015},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test: %d", i), func(t *testing.T) {
			years := time.Duration(tc.year * NanosecondsPerYear * int64(time.Nanosecond))
			blockTime := genesisTime.Add(years)
			ctx := sdk.NewContext(nil, tmproto.Header{}, false, nil).WithBlockTime(blockTime)
			inflationRate := minter.CalculateInflationRate(ctx, genesisTime)
			got, err := inflationRate.Float64()
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got, "want %v got %v year %v blockTime %v", tc.want, got, tc.year, blockTime)
		})
	}
}

func TestCalculateBlockProvision(t *testing.T) {
	minter := DefaultMinter()
	current := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	blockInterval := 15 * time.Second
	totalSupply := math.LegacyNewDec(1_000_000_000_000)              // 1 trillion utia
	annualProvisions := totalSupply.Mul(InitialInflationRateAsDec()) // 53.6 billion utia

	type testCase struct {
		name             string
		annualProvisions math.LegacyDec
		current          time.Time
		previous         time.Time
		want             sdk.Coin
		wantErr          bool
	}

	testCases := []testCase{
		{
			name:             "one 15 second block during the first year",
			annualProvisions: annualProvisions,
			current:          current,
			previous:         current.Add(-blockInterval),
			// 53.6 billion utia (annual provisions) * 15 (seconds) / 31,556,952 (seconds per year) = 25477 utia (truncated)
			want: sdk.NewCoin(params.BondDenom, math.NewInt(25477)),
		},
		{
			name:             "one 30 second block during the first year",
			annualProvisions: annualProvisions,
			current:          current,
			previous:         current.Add(-2 * blockInterval),
			// 53.6 billion utia (annual provisions) * 30 (seconds) / 31,556,952 (seconds per year) = 50955 utia (truncated)
			want: sdk.NewCoin(params.BondDenom, math.NewInt(50955)),
		},
		{
			name:             "want error when current time is before previous time",
			annualProvisions: annualProvisions,
			current:          current,
			previous:         current.Add(blockInterval),
			wantErr:          true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			minter.AnnualProvisions = tc.annualProvisions
			got, err := minter.CalculateBlockProvision(tc.current, tc.previous)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			require.True(t, tc.want.IsEqual(got), "want %v got %v", tc.want, got)
		})
	}
}

// TestCalculateBlockProvisionError verifies that the error for total block
// provisions in a year is less than .01
func TestCalculateBlockProvisionError(t *testing.T) {
	minter := DefaultMinter()
	current := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	oneYear := time.Duration(NanosecondsPerYear)
	end := current.Add(oneYear)

	totalSupply := math.LegacyNewDec(1_000_000_000_000)              // 1 trillion utia
	annualProvisions := totalSupply.Mul(InitialInflationRateAsDec()) // 53.6 billion utia
	minter.AnnualProvisions = annualProvisions
	totalBlockProvisions := math.LegacyNewDec(0)
	for current.Before(end) {
		blockInterval := randomBlockInterval()
		previous := current
		current = current.Add(blockInterval)
		got, err := minter.CalculateBlockProvision(current, previous)
		require.NoError(t, err)
		totalBlockProvisions = totalBlockProvisions.Add(math.LegacyNewDecFromInt(got.Amount))
	}

	gotError := totalBlockProvisions.Sub(annualProvisions).Abs().Quo(annualProvisions)
	wantError := math.LegacyNewDecWithPrec(1, 2) // .01
	assert.True(t, gotError.LTE(wantError))
}

func randomBlockInterval() time.Duration {
	rangeMin := (14 * time.Second).Nanoseconds()
	rangeMax := (16 * time.Second).Nanoseconds()
	return time.Duration(randInRange(rangeMin, rangeMax))
}

// randInRange returns a random number in the range (min, max) inclusive.
func randInRange(rangeMin, rangeMax int64) int64 {
	return rand.Int63n(rangeMax-rangeMin) + rangeMin
}

func BenchmarkCalculateBlockProvision(b *testing.B) {
	b.ReportAllocs()
	minter := DefaultMinter()

	s1 := rand.NewSource(100)
	r1 := rand.New(s1)
	minter.AnnualProvisions = math.LegacyNewDec(r1.Int63n(1000000))
	current := time.Unix(r1.Int63n(1000000), 0)
	previous := current.Add(-time.Second * 15)

	for n := 0; n < b.N; n++ {
		_, err := minter.CalculateBlockProvision(current, previous)
		require.NoError(b, err)
	}
}

func BenchmarkCalculateInflationRate(b *testing.B) {
	b.ReportAllocs()
	minter := DefaultMinter()
	genesisTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	for n := 0; n < b.N; n++ {
		ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
		minter.CalculateInflationRate(ctx, genesisTime)
	}
}

func TestYearsSinceGenesis(t *testing.T) {
	type testCase struct {
		name    string
		current time.Time
		want    int64
	}

	genesis := time.Date(2023, 1, 1, 12, 30, 15, 0, time.UTC) // 2023-01-01T12:30:15Z
	oneDay, err := time.ParseDuration("24h")
	assert.NoError(t, err)
	oneWeek := oneDay * 7
	oneMonth := oneDay * 30
	oneYear := time.Duration(NanosecondsPerYear)
	twoYears := 2 * oneYear
	tenYears := 10 * oneYear
	tenYearsOneMonth := tenYears + oneMonth

	testCases := []testCase{
		{
			name:    "one day after genesis",
			current: genesis.Add(oneDay),
			want:    0,
		},
		{
			name:    "one day before genesis",
			current: genesis.Add(-oneDay),
			want:    0,
		},
		{
			name:    "one week after genesis",
			current: genesis.Add(oneWeek),
			want:    0,
		},
		{
			name:    "one month after genesis",
			current: genesis.Add(oneMonth),
			want:    0,
		},
		{
			name:    "one year after genesis",
			current: genesis.Add(oneYear),
			want:    1,
		},
		{
			name:    "two years after genesis",
			current: genesis.Add(twoYears),
			want:    2,
		},
		{
			name:    "ten years after genesis",
			current: genesis.Add(tenYears),
			want:    10,
		},
		{
			name:    "ten years and one month after genesis",
			current: genesis.Add(tenYearsOneMonth),
			want:    10,
		},
	}

	for _, tc := range testCases {
		got := yearsSinceGenesis(genesis, tc.current)
		assert.Equal(t, tc.want, got, tc.name)
	}
}
