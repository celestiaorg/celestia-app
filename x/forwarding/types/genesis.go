package types

import "errors"

// DefaultGenesis returns the default genesis state for the forwarding module.
// The module is stateless - funds are tracked by the bank module at derived addresses.
func DefaultGenesis() *GenesisState {
	return &GenesisState{}
}

// Validate performs basic genesis state validation.
func (gs GenesisState) Validate() error {
	return nil
}

// ValidateGenesis validates genesis state.
// Returns error if genesis is nil.
func ValidateGenesis(gs *GenesisState) error {
	if gs == nil {
		return errors.New("genesis state cannot be nil")
	}
	return gs.Validate()
}
