package networks

import (
	"os"

	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
)

// Config holds the configuration for connecting to an existing live chain
type Config struct {
	Name    string
	ChainID string
	RPCs    []string
	GRPCs []string
	Seeds string
}

// NewMochaConfig returns a Config for the mocha testnet
func NewMochaConfig() *Config {
	return &Config{
		Name:    "mocha",
		ChainID: appconsts.MochaChainID,
		// State sync requires >= 2 RPC servers to cross-verify the app hash
		// header. These must be distinct providers: listing one host twice
		// gives no redundancy, so a single slow/unavailable provider stalls
		// state sync. Keep these in sync with the live mocha-4 testnet.
		RPCs: []string{
			"https://celestia-testnet-rpc.itrocket.net:443",
			"https://rpc-mocha.pops.one:443",
			"https://full.consensus.mocha-4.celestia-mocha.com:443",
		},
		// seeds provide dynamic peer discovery — the node contacts a seed,
		// gets a fresh list of currently-alive peers, and connects. This is
		// more resilient than hardcoded persistent peers which go stale.
		Seeds: "b402fe40f3474e9e208840702e1b7aa37f2edc4b@celestia-testnet-seed.itrocket.net:14656,ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@seed-mocha.pops.one:26656",
	}
}

// NewArabicaConfig returns a Config for the arabica devnet.
// RPCs can be overridden via ARABICA_RPC env var.
func NewArabicaConfig() *Config {
	cfg := &Config{
		Name:    "arabica",
		ChainID: appconsts.ArabicaChainID,
		RPCs:    []string{"https://rpc.celestia-arabica-11.com:443"},
		GRPCs:   []string{"rpc.celestia-arabica-11.com:9090"},
	}
	if rpc := os.Getenv("ARABICA_RPC"); rpc != "" {
		cfg.RPCs = []string{rpc}
	}
	return cfg
}

// TODO: add additional config for mainnet
// func NewMainnetConfig() *Config {}
