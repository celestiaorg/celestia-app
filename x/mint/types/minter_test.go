package types

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestNextInflationRate(t *testing.T) {
	minter := DefaultMinter()

	type testCase struct {
		year int
		want float64
	}

	testCases := []testCase{
		{0, 0.08},
		{1, 0.072},
		{2, 0.0648},
		{3, 0.05832},
		{4, 0.052488},
		{5, 0.0472392},
		{6, 0.04251528},
		{7, 0.038263752},
		{8, 0.0344373768},
		{9, 0.03099363912},
		{10, 0.027894275208},
		{11, 0.0251048476872},
		{12, 0.02259436291848},
		{13, 0.020334926626632},
		{14, 0.0183014339639688},
		{15, 0.01647129056757192},
		{16, 0.0150},
		{17, 0.0150},
		{18, 0.0150},
		{19, 0.0150},
		{20, 0.0150},
		{21, 0.0150},
		{22, 0.0150},
		{23, 0.0150},
		{24, 0.0150},
		{25, 0.0150},
		{26, 0.0150},
		{27, 0.0150},
		{28, 0.0150},
		{29, 0.0150},
		{30, 0.0150},
		{31, 0.0150},
		{32, 0.0150},
		{33, 0.0150},
		{34, 0.0150},
		{35, 0.0150},
		{36, 0.0150},
		{37, 0.0150},
		{38, 0.0150},
		{39, 0.0150},
		{40, 0.0150},
	}

	for _, tc := range testCases {
		height := BlocksPerYear * tc.year
		ctx := sdk.NewContext(nil, tmproto.Header{Height: int64(height)}, false, nil)
		inflationRate := minter.InflationRate(ctx)
		got, err := inflationRate.Float64()
		assert.NoError(t, err)
		assert.Equal(t, tc.want, got, "want %v got %v year %v height %v", tc.want, got, tc.year, height)
	}
}

func TestBlockProvision(t *testing.T) {
	minter := DefaultMinter()

	type testCase struct {
		annualProvisions int64
		want             sdk.Coin
	}
	testCases := []testCase{
		{
			annualProvisions: BlocksPerYear,
			want:             sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(1)),
		},
		{
			annualProvisions: BlocksPerYear * 2,
			want:             sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(2)),
		},
		{
			annualProvisions: (BlocksPerYear * 10) - 1,
			want:             sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(9)),
		},
		{
			annualProvisions: BlocksPerYear / 2,
			want:             sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(0)),
		},
	}
	for _, tc := range testCases {
		minter.AnnualProvisions = sdk.NewDec(tc.annualProvisions)
		got := minter.BlockProvision()
		require.True(t, tc.want.IsEqual(got), "want %v got %v", tc.want, got)
	}
}

// Benchmarking :)
// previously using math.Int operations:
// BenchmarkBlockProvision-4 5000000 220 ns/op
//
// using sdk.Dec operations: (current implementation)
// BenchmarkBlockProvision-4 3000000 429 ns/op
func BenchmarkBlockProvision(b *testing.B) {
	b.ReportAllocs()
	minter := DefaultMinter()

	s1 := rand.NewSource(100)
	r1 := rand.New(s1)
	minter.AnnualProvisions = sdk.NewDec(r1.Int63n(1000000))

	// run the BlockProvision function b.N times
	for n := 0; n < b.N; n++ {
		minter.BlockProvision()
	}
}

// Next inflation benchmarking
// BenchmarkNextInflation-4 1000000 1828 ns/op
func BenchmarkNextInflation(b *testing.B) {
	b.ReportAllocs()
	minter := DefaultMinter()

	// run the NextInflationRate function b.N times
	for n := 0; n < b.N; n++ {
		ctx := sdk.NewContext(nil, tmproto.Header{Height: int64(n)}, false, nil)
		minter.InflationRate(ctx)
	}
}

// Next annual provisions benchmarking
// BenchmarkNextAnnualProvisions-4 5000000 251 ns/op
func BenchmarkNextAnnualProvisions(b *testing.B) {
	b.ReportAllocs()
	minter := DefaultMinter()
	params := DefaultParams()
	totalSupply := sdk.NewInt(100000000000000)

	// run the NextAnnualProvisions function b.N times
	for n := 0; n < b.N; n++ {
		minter.NextAnnualProvisions(params, totalSupply)
	}
}
