package types

import (
	"fmt"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewMinter returns a new Minter object.
func NewMinter(inflationRate sdk.Dec, annualProvisions sdk.Dec, genesisTime *time.Time, bondDenom string) Minter {
	return Minter{
		InflationRate:    inflationRate,
		AnnualProvisions: annualProvisions,
		GenesisTime:      genesisTime,
		BondDenom:        bondDenom,
	}
}

// DefaultMinter returns a Minter object with default values.
func DefaultMinter() Minter {
	unixEpoch := time.Unix(0, 0).UTC()
	return NewMinter(initalInflationRate, sdk.NewDec(0), &unixEpoch, sdk.DefaultBondDenom)
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
func (m Minter) CalculateInflationRate(ctx sdk.Context) sdk.Dec {
	years := yearsSinceGenesis(*m.GenesisTime, ctx.BlockTime())
	inflationRate := initalInflationRate.Mul(sdk.OneDec().Sub(disinflationRate).Power(uint64(years)))

	if inflationRate.LT(targetInflationRate) {
		return targetInflationRate
	}
	return inflationRate
}

// CalculateAnnualProvisions returns the total number of tokens that should be
// minted due to inflation for the current year.
func (m Minter) CalculateAnnualProvisions(totalSupply math.Int) sdk.Dec {
	return m.InflationRate.MulInt(totalSupply)
}

// CalculateBlockProvision returns the total number of coins that should be
// minted due to inflation for the current block.
func (m Minter) CalculateBlockProvision(current time.Time, previous time.Time) sdk.Coin {
	timeElapsed := current.Sub(previous).Nanoseconds()
	portionOfYear := sdk.NewDec(int64(timeElapsed)).Quo(sdk.NewDec(int64(NanosecondsPerYear)))
	blockProvision := m.AnnualProvisions.Mul(portionOfYear)
	return sdk.NewCoin(m.BondDenom, blockProvision.TruncateInt())
}

// yearsSinceGenesis returns the number of years that have passed between
// genesis and current (rounded down).
func yearsSinceGenesis(genesis time.Time, current time.Time) (years int64) {
	if current.Before(genesis) {
		return 0
	}
	return current.Sub(genesis).Nanoseconds() / int64(NanosecondsPerYear)
}
