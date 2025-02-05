package types

import (
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const DefaultBondDenom = "utia"

// NewMinter returns a new Minter object.
func NewMinter(inflationRate sdk.Dec, annualProvisions sdk.Dec, bondDenom string) Minter {
	return Minter{
		InflationRate:    inflationRate,
		AnnualProvisions: annualProvisions,
		BondDenom:        bondDenom,
	}
}

// DefaultMinter returns a Minter object with default values.
func DefaultMinter() Minter {
	annualProvisions := sdk.NewDec(0)
	return NewMinter(InitialInflationRateAsDec(), annualProvisions, DefaultBondDenom)
}

// Validate returns an error if the minter is invalid.
func (m Minter) Validate() error {
	if m.InflationRate.IsNegative() {
		return fmt.Errorf("inflation rate %v should be positive", m.InflationRate.String())
	}
	if m.AnnualProvisions.IsNegative() {
		return fmt.Errorf("annual provisions %v should be positive", m.AnnualProvisions.String())
	}
	if m.BondDenom == "" {
		return fmt.Errorf("bond denom should not be empty string")
	}
	return nil
}

// CalculateInflationRate returns the inflation rate for the current year depending on
// the current block height in context. The inflation rate is expected to
// decrease every year according to the schedule specified in the README.
func (m Minter) CalculateInflationRate(ctx sdk.Context, genesis time.Time) sdk.Dec {
	if ctx.ConsensusParams().Version.AppVersion <= 3 {
		return calculateInflationRatePreCip29(ctx, genesis)
	} else {
		return calculateInflationRatePostCip29(ctx, genesis)
	}
}

func calculateInflationRatePreCip29(ctx sdk.Context, genesis time.Time) sdk.Dec {
	years := yearsSinceGenesis(genesis, ctx.BlockTime())
	inflationRate := InitialInflationRateAsDec().Mul(sdk.OneDec().Sub(DisinflationRateAsDec()).Power(uint64(years)))

	if inflationRate.LT(TargetInflationRateAsDec()) {
		return TargetInflationRateAsDec()
	}
	return inflationRate
}

func calculateInflationRatePostCip29(ctx sdk.Context, genesis time.Time) sdk.Dec {
	years := yearsSinceGenesis(genesis, ctx.BlockTime())
	inflationRate := InitialInflationRateCip29AsDec().Mul(sdk.OneDec().Sub(DisinflationRateCip29AsDec()).Power(uint64(years)))

	if inflationRate.LT(TargetInflationRateAsDec()) {
		return TargetInflationRateAsDec()
	}
	return inflationRate
}

// CalculateBlockProvision returns the total number of coins that should be
// minted due to inflation for the current block.
func (m Minter) CalculateBlockProvision(current time.Time, previous time.Time) (sdk.Coin, error) {
	if current.Before(previous) {
		return sdk.Coin{}, fmt.Errorf("current time %v cannot be before previous time %v", current, previous)
	}
	timeElapsed := current.Sub(previous).Nanoseconds()
	portionOfYear := sdk.NewDec(timeElapsed).Quo(sdk.NewDec(NanosecondsPerYear))
	blockProvision := m.AnnualProvisions.Mul(portionOfYear)
	return sdk.NewCoin(m.BondDenom, blockProvision.TruncateInt()), nil
}

// yearsSinceGenesis returns the number of years that have passed between
// genesis and current (rounded down).
func yearsSinceGenesis(genesis time.Time, current time.Time) (years int64) {
	if current.Before(genesis) {
		return 0
	}
	return current.Sub(genesis).Nanoseconds() / NanosecondsPerYear
}
