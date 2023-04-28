package types

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestNextInflation(t *testing.T) {
	minter := DefaultInitialMinter()
	params := DefaultParams()

	tests := []struct {
		year             int
		expInflationRate float64
		// tokensIssued     uint64
		// totalSupply      uint64
	}{
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

	for i, tc := range tests {
		targetHeight := BlocksPerYear * uint64(tc.year)

		ctx := sdk.NewContext(nil, tmproto.Header{Height: int64(targetHeight)}, false, nil)

		inflation := minter.NextInflationRate(ctx, params)

		infRate, err := inflation.Float64()
		require.NoError(t, err)

		require.Equal(t, tc.expInflationRate, infRate,
			"Test Index: %v\nTarget year: %v\nTarget height: %v\nGot Rate: %v\nExpected Rate: %v\n", i, tc.year, targetHeight, inflation, tc.expInflationRate)

	}
}

func TestBlockProvision(t *testing.T) {
	minter := InitialMinter(sdk.NewDecWithPrec(1, 1))
	params := DefaultParams()

	secondsPerYear := int64(60 * 60 * 8766)

	tests := []struct {
		annualProvisions int64
		expProvisions    int64
	}{
		{secondsPerYear / 5, 1},
		{secondsPerYear/5 + 1, 1},
		{(secondsPerYear / 5) * 2, 2},
		{(secondsPerYear / 5) / 2, 0},
	}
	for i, tc := range tests {
		minter.AnnualProvisions = sdk.NewDec(tc.annualProvisions)
		provisions := minter.BlockProvision(params)

		expProvisions := sdk.NewCoin(sdk.DefaultBondDenom,
			sdk.NewInt(tc.expProvisions))

		require.True(t, expProvisions.IsEqual(provisions),
			"test: %v\n\tExp: %v\n\tGot: %v\n",
			i, tc.expProvisions, provisions)
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
	minter := InitialMinter(sdk.NewDecWithPrec(1, 1))
	params := DefaultParams()

	s1 := rand.NewSource(100)
	r1 := rand.New(s1)
	minter.AnnualProvisions = sdk.NewDec(r1.Int63n(1000000))

	// run the BlockProvision function b.N times
	for n := 0; n < b.N; n++ {
		minter.BlockProvision(params)
	}
}

// Next inflation benchmarking
// BenchmarkNextInflation-4 1000000 1828 ns/op
func BenchmarkNextInflation(b *testing.B) {
	b.ReportAllocs()
	minter := InitialMinter(sdk.NewDecWithPrec(1, 1))
	params := DefaultParams()

	// run the NextInflationRate function b.N times
	for n := 0; n < b.N; n++ {
		ctx := sdk.NewContext(nil, tmproto.Header{Height: int64(n)}, false, nil)
		minter.NextInflationRate(ctx, params)
	}
}

// Next annual provisions benchmarking
// BenchmarkNextAnnualProvisions-4 5000000 251 ns/op
func BenchmarkNextAnnualProvisions(b *testing.B) {
	b.ReportAllocs()
	minter := InitialMinter(sdk.NewDecWithPrec(1, 1))
	params := DefaultParams()
	totalSupply := sdk.NewInt(100000000000000)

	// run the NextAnnualProvisions function b.N times
	for n := 0; n < b.N; n++ {
		minter.NextAnnualProvisions(params, totalSupply)
	}
}
