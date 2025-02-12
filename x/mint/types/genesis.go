package types

import "errors"

// NewGenesisState creates a new GenesisState object
func NewGenesisState(bondDenom string) *GenesisState {
	return &GenesisState{
		BondDenom: bondDenom,
	}
}

// DefaultGenesisState creates a default GenesisState object
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		BondDenom: DefaultBondDenom,
	}
}

// ValidateGenesis validates the provided genesis state to ensure the
// expected invariants hold.
func ValidateGenesis(data GenesisState) error {
	if data.BondDenom == "" {
		return errors.New("bond denom cannot be empty")
	}
	return nil
}
