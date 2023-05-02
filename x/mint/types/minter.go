package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewMinter returns a new Minter object.
func NewMinter(inflation sdk.Dec, annualProvisions sdk.Dec) Minter {
	return Minter{
		Inflation:        inflation,
		AnnualProvisions: annualProvisions,
	}
}

// DefaultMinter returns a Minter object with default values.
func DefaultMinter() Minter {
	return NewMinter(initalInflationRate, sdk.NewDec(0))
}

// ValidateMinter returns an error if the provided minter is invalid.
func ValidateMinter(minter Minter) error {
	if minter.Inflation.IsNegative() {
		return fmt.Errorf("minter inflation %v should be positive", minter.Inflation.String())
	}
	if minter.AnnualProvisions.IsNegative() {
		return fmt.Errorf("minter annual provisions %v should be positive", minter.AnnualProvisions.String())
	}
	return nil
}

// InflationRate returns the inflation rate for the current year depending on
// the current block height in context. The inflation rate is expected to
// decrease every year according to the schedule specified in the README.
func (m Minter) InflationRate(ctx sdk.Context) sdk.Dec {
	year := uint64(ctx.BlockHeader().Height) / BlocksPerYear
	inflationRate := initalInflationRate.Mul(sdk.OneDec().Sub(disinflationRate).Power(year))

	if inflationRate.LT(targetInflationRate) {
		return targetInflationRate
	}
	return inflationRate
}

// NextAnnualProvisions returns the annual provisions based on current total
// supply and inflation rate.
func (m Minter) NextAnnualProvisions(totalSupply math.Int) sdk.Dec {
	return m.Inflation.MulInt(totalSupply)
}

// BlockProvision returns the provisions for a block based on the annual
// provisions rate.
func (m Minter) BlockProvision() sdk.Coin {
	provisionAmt := m.AnnualProvisions.QuoInt(sdk.NewInt(int64(BlocksPerYear)))
	return sdk.NewCoin(sdk.DefaultBondDenom, provisionAmt.TruncateInt())
}
