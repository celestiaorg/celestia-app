package types

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

// ValidateGenesis validates genesis state
func ValidateGenesis(gs *GenesisState) error {
	if gs == nil {
		return nil
	}
	return gs.Validate()
}
