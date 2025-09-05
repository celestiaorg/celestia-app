package types

// DefaultGenesis returns the default module genesis.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
	}
}
