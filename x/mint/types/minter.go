package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewMinter returns a new Minter object with the given inflation and annual
// provisions values.
func NewMinter(inflation, annualProvisions sdk.Dec) Minter {
	return Minter{
		Inflation:        inflation,
		AnnualProvisions: annualProvisions,
	}
}

// InitialMinter returns an initial Minter object with a given inflation value.
func InitialMinter(inflation sdk.Dec) Minter {
	return NewMinter(
		inflation,
		sdk.NewDec(0),
	)
}

// DefaultInitialMinter returns a default initial Minter object for a new chain
// which uses an inflation rate of 13%.
func DefaultInitialMinter() Minter {
	return InitialMinter(
		sdk.NewDecWithPrec(13, 2),
	)
}

// validate minter
func ValidateMinter(minter Minter) error {
	if minter.Inflation.IsNegative() {
		return fmt.Errorf("mint parameter Inflation should be positive, is %s",
			minter.Inflation.String())
	}
	return nil
}

// NextInflationRate returns the next inflation rate.
func (m Minter) NextInflationRate(ctx sdk.Context, params Params) sdk.Dec {
	year := uint64(ctx.BlockHeader().Height) / BlocksPerYear

	initInflationRate := sdk.NewDecWithPrec(InitialInflationRate*1000, 3)
	targetInflationRate := sdk.NewDecWithPrec(TargetInflationRate*1000, 3)

	// nextInflationRate = initInflationRate * ((1 - DisinflationRate) ^ year)
	nextInflationRate := initInflationRate.Mul(
		sdk.OneDec().Sub(
			sdk.NewDecWithPrec(DisinflationRatePerYear*1000, 3)).
			Power(year))

	if nextInflationRate.LT(targetInflationRate) {
		return targetInflationRate
	}
	return nextInflationRate
}

// NextAnnualProvisions returns the annual provisions based on current total
// supply and inflation rate.
func (m Minter) NextAnnualProvisions(_ Params, totalSupply math.Int) sdk.Dec {
	return m.Inflation.MulInt(totalSupply)
}

// BlockProvision returns the provisions for a block based on the annual
// provisions rate.
func (m Minter) BlockProvision(params Params) sdk.Coin {
	provisionAmt := m.AnnualProvisions.QuoInt(sdk.NewInt(int64(BlocksPerYear)))
	return sdk.NewCoin(sdk.DefaultBondDenom, provisionAmt.TruncateInt())
}
