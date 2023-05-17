package types

// NewGenesisState creates a new GenesisState object
func NewGenesisState(minter Minter) *GenesisState {
	return &GenesisState{
		Minter: minter,
	}
}

// DefaultGenesisState creates a default GenesisState object
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Minter: DefaultMinter(),
	}
}

// ValidateGenesis validates the provided genesis state to ensure the
// expected invariants holds.
func ValidateGenesis(data GenesisState) error {
	return data.Minter.Validate()
}
