package networks

import (
	"fmt"
	"os"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
)

// Config holds the configuration for connecting to an existing live chain
type Config struct {
	Name    string
	ChainID string
	RPCs    []string
	GRPCs   []string
	Seeds   string
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

// NewCortoConfig returns a Config for the Corto internal testnet. Corto has
// no public endpoints, so the RPC and gRPC endpoints must be provided via the
// CORTO_RPC and CORTO_GRPC env vars.
func NewCortoConfig() (*Config, error) {
	rpc := os.Getenv("CORTO_RPC")
	if rpc == "" {
		return nil, fmt.Errorf("CORTO_RPC environment variable must be set")
	}
	grpc := os.Getenv("CORTO_GRPC")
	if grpc == "" {
		return nil, fmt.Errorf("CORTO_GRPC environment variable must be set")
	}
	return &Config{
		Name:    "corto",
		ChainID: appconsts.CortoChainID,
		RPCs:    []string{rpc},
		GRPCs:   []string{grpc},
	}, nil
}

// TODO: add additional config for mainnet
// func NewMainnetConfig() *Config {}
