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

// InflationRate returns the inflation rate for the current year depending on
// the current block height in context.
func (m Minter) InflationRate(ctx sdk.Context) sdk.Dec {
	year := uint64(ctx.BlockHeader().Height) / BlocksPerYear
	inflationRate := initInflationRate.Mul(sdk.OneDec().Sub(disinflationRate).Power(year))

	if inflationRate.LT(targetInflationRate) {
		return targetInflationRate
	}
	return inflationRate
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
