package types

import "errors"

// DefaultGenesis returns the default genesis state for the forwarding module
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
	}
}

// NewGenesisState creates a new GenesisState instance
func NewGenesisState(params Params) *GenesisState {
	return &GenesisState{
		Params: params,
	}
}

// Validate performs basic genesis state validation
func (gs GenesisState) Validate() error {
	return gs.Params.Validate()
}

// ValidateGenesis validates genesis state.
// Returns error if genesis is nil or invalid.
func ValidateGenesis(gs *GenesisState) error {
	if gs == nil {
		return errors.New("genesis state cannot be nil")
	}
	return gs.Validate()
}
