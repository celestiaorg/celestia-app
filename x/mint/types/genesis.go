package types

import (
	"errors"
	"fmt"

	"github.com/celestiaorg/celestia-app/v11/app/params"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(bondDenom string) *GenesisState {
	return &GenesisState{
		BondDenom: bondDenom,
	}
}

// DefaultGenesisState creates a default GenesisState object
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		BondDenom: params.BondDenom,
	}
}

// ValidateGenesis validates the provided genesis state to ensure the
// expected invariants hold.
func ValidateGenesis(data GenesisState) error {
	if data.BondDenom == "" {
		return errors.New("bond denom cannot be empty")
	}
	if (data.Minter == nil) != (data.GenesisTime == nil) {
		return errors.New("minter and genesis time must be set together")
	}
	if data.Minter == nil {
		return nil
	}
	if data.Minter.InflationRate.IsNil() || data.Minter.AnnualProvisions.IsNil() {
		return errors.New("minter inflation rate and annual provisions must be set")
	}
	if err := data.Minter.Validate(); err != nil {
		return err
	}
	if data.Minter.BondDenom != data.BondDenom {
		return fmt.Errorf("minter bond denom %q does not match bond denom %q", data.Minter.BondDenom, data.BondDenom)
	}
	return nil
}
