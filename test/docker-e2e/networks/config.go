package networks

import "github.com/celestiaorg/celestia-app/v8/pkg/appconsts"

// Config holds the configuration for connecting to an existing live chain
type Config struct {
	Name    string
	ChainID string
	RPCs    []string
	Seeds   string
	Peers   string
}

// NewMochaConfig returns a Config for the mocha testnet
func NewMochaConfig() *Config {
	return &Config{
		Name:    "mocha",
		ChainID: appconsts.MochaChainID,
		RPCs:    []string{"https://celestia-testnet-rpc.itrocket.net:443", "https://celestia-testnet-rpc.itrocket.net:443"},
		Seeds:   "b402fe40f3474e9e208840702e1b7aa37f2edc4b@celestia-testnet-seed.itrocket.net:14656,ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@seed-mocha.pops.one:26656",
		Peers:   "daf2cecee2bd7f1b3bf94839f993f807c6b15fbf@celestia-testnet-peer.itrocket.net:11656,96b2761729cea90ee7c61206433fc0ba40c245bf@57.128.141.126:11656,f4f75a55bfc5f302ef34435ef096a4551ecb6804@152.53.33.96:12056,31bb1c9c1be7743d1115a8270bd1c83d01a9120a@148.72.141.31:26676,3e30bcfc55e7d351f18144aab4b0973e9e9bf987@65.108.226.183:11656,7a0d5818c0e5b0d4fbd86a9921f413f5e4e4ac1e@65.109.83.40:28656,43e9da043318a4ea0141259c17fcb06ecff816af@164.132.247.253:43656,5a7566aa030f7e5e7114dc9764f944b2b1324bcd@65.109.23.114:11656,c17c0cbf05e98656fee5f60fad469fc528f6d6de@65.109.25.113:11656,fb5e0b9efacc11916c58bbcd3606cbaa7d43c99f@65.108.234.84:28656,45504fb31eb97ea8778c920701fc8076e568a9cd@188.214.133.100:26656,edafdf47c443344fb940a32ab9d2067c482e59df@84.32.71.47:26656,ae7d00d6d70d9b9118c31ac0913e0808f2613a75@177.54.156.69:26656,7c841f59c35d70d9f1472d7d2a76a11eefb7f51f@136.243.69.100:43656",
	}
}

// TODO: add additional configs for mainnet, arabica
// func NewArabicaConfig() *Config {}
// func NewMainnetConfig() *Config {}
