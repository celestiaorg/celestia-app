package types

import (
	"errors"

	"github.com/celestiaorg/celestia-app/v4/app/params"
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
// expected invariants holds.
func ValidateGenesis(data GenesisState) error {
	if data.BondDenom == "" {
		return errors.New("bond denom cannot be empty")
	}
	return nil
}
