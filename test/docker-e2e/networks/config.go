package networks

import "github.com/celestiaorg/celestia-app/v6/pkg/appconsts"

// Config holds the configuration for connecting to an existing live chain
type Config struct {
	Name    string
	ChainID string
	RPCs    []string
	Seeds   string
}

// NewMochaConfig returns a Config for the mocha testnet
func NewMochaConfig() *Config {
	return &Config{
		Name:    "mocha",
		ChainID: appconsts.MochaChainID,
		RPCs:    []string{"https://celestia-testnet-rpc.itrocket.net:443", "https://celestia-testnet-rpc.itrocket.net:443"},
		Seeds:   "5d0bf034d6e6a8b5ee31a2f42f753f1107b3a00e@celestia-testnet-seed.itrocket.net:11656",
	}
}

// TODO: add additional configs for mainnet, arabica
// func NewArabicaConfig() *Config {}
// func NewMainnetConfig() *Config {}
