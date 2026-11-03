package types

import (
	"math/big"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// FuzzMinterBlockProvision drives the minter with proposer-influenced block
// times: genesis, previous and current block times anywhere in the int64
// nanosecond range, and any annual provisions up to 2^96 utia. The block
// provision must fail exactly when the current time is before the previous
// one, otherwise equal floor(annual * elapsed / year) computed with big.Int up
// to LegacyDec's 18-decimal rounding of the year fraction, never be negative,
// and never decrease when more time elapses. The inflation rate must stay
// between the target and initial rates and never rise from one year to the
// next.
func FuzzMinterBlockProvision(f *testing.F) {
	year := NanosecondsPerYear
	f.Add(int64(0), int64(0), int64(15_000_000_000), uint64(1_000_000), uint32(0))
	f.Add(int64(0), year, year+6_000_000_000, uint64(80_000_000_000_000), uint32(1))
	f.Add(int64(0), 10*year, 9*year, uint64(1), uint32(0))
	f.Add(int64(-1<<63), int64(-1<<63), int64(1<<63-1), ^uint64(0), ^uint32(0))
	f.Add(int64(1<<62), int64(0), 30*year, uint64(1<<40), uint32(7))

	f.Fuzz(func(t *testing.T, genesisNs, previousNs, currentNs int64, annualLow uint64, annualHigh uint32) {
		annual := new(big.Int).Lsh(new(big.Int).SetUint64(uint64(annualHigh)), 64)
		annual.Or(annual, new(big.Int).SetUint64(annualLow))
		minter := DefaultMinter()
		minter.AnnualProvisions = math.LegacyNewDecFromBigInt(annual)

		previous, current := time.Unix(0, previousNs).UTC(), time.Unix(0, currentNs).UTC()
		coin, err := minter.CalculateBlockProvision(current, previous)
		if current.Before(previous) {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, minter.BondDenom, coin.Denom)
			require.False(t, coin.Amount.IsNegative())

			elapsed := big.NewInt(current.Sub(previous).Nanoseconds())
			exact := new(big.Int).Mul(annual, elapsed)
			exact.Quo(exact, big.NewInt(year))
			// The year fraction is rounded to 18 decimals before multiplying,
			// which can move the result by up to annual * 1e-18 + 1.
			slack := new(big.Int).Quo(annual, big.NewInt(1_000_000_000_000_000_000))
			slack.Add(slack, big.NewInt(1))
			diff := new(big.Int).Sub(coin.Amount.BigInt(), exact)
			require.LessOrEqual(t, diff.CmpAbs(slack), 0, "provision %s, exact %s", coin.Amount, exact)

			// One more second never mints less.
			later := current.Add(time.Second)
			if later.After(current) {
				more, err := minter.CalculateBlockProvision(later, previous)
				require.NoError(t, err)
				require.True(t, more.Amount.GTE(coin.Amount), "more time minted less: %s < %s", more.Amount, coin.Amount)
			}
		}

		genesis := time.Unix(0, genesisNs).UTC()
		rate := minter.CalculateInflationRate(sdk.Context{}.WithBlockTime(current), genesis)
		require.True(t, rate.GTE(TargetInflationRateAsDec()), "rate %s below target", rate)
		require.True(t, rate.LTE(InitialInflationRateAsDec()), "rate %s above initial", rate)
		nextYear := current.Add(time.Duration(year))
		if nextYear.After(current) {
			next := minter.CalculateInflationRate(sdk.Context{}.WithBlockTime(nextYear), genesis)
			require.True(t, next.LTE(rate), "inflation rose from %s to %s a year later", rate, next)
		}
	})
}
