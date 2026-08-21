package networks

import "github.com/celestiaorg/celestia-app/v9/pkg/appconsts"

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
		// State sync requires >= 2 RPC servers to cross-verify the app hash
		// header. These should be distinct providers: listing one host twice
		// gives no redundancy, so a single slow/unavailable provider stalls
		// state sync. Keep these in sync with the live mocha-5 testnet.
		// TODO: replace the duplicate entry with a second distinct provider
		// once more RPC providers serve mocha-5.
		RPCs: []string{
			"https://rpc-mocha.pops.one:443",
			"https://rpc-mocha.pops.one:443",
		},
		// seeds provide dynamic peer discovery — the node contacts a seed,
		// gets a fresh list of currently-alive peers, and connects. This is
		// more resilient than hardcoded persistent peers which go stale.
		// Keep in sync with https://github.com/celestiaorg/networks/blob/main/mocha-5/seeds.txt
		Seeds: "ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@84.32.215.148:26656",
	}
}

// TODO: add additional configs for mainnet, arabica
// func NewArabicaConfig() *Config {}
// func NewMainnetConfig() *Config {}
