package types

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{}
}

// ValidateGenesis performs basic genesis state validation.
func ValidateGenesis(_ *GenesisState) error {
	return nil
}
