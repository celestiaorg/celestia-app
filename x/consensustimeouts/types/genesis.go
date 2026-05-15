package types

// NewGenesisState constructs a GenesisState with the supplied Params.
func NewGenesisState(params Params) *GenesisState {
	return &GenesisState{Params: params}
}

// DefaultGenesis returns a GenesisState populated with the module's default params.
func DefaultGenesis() *GenesisState {
	return NewGenesisState(DefaultParams())
}

// Validate checks the GenesisState's Params are within bounds.
func (g GenesisState) Validate() error {
	return g.Params.Validate()
}
