package networks

import (
	"fmt"
	"os"
	"strings"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
)

// Config holds the configuration for connecting to an existing live chain
type Config struct {
	Name    string
	ChainID string
	RPCs    []string
	GRPCs   []string
	// AuthToken, if set, authenticates both connections: it is attached as an
	// x-token header on gRPC calls and as an Authorization Bearer header on
	// RPC requests.
	AuthToken string
	Seeds     string
	// Peers are persistent peers that keep the full block history. Block
	// syncing from genesis needs at least one of them because most public
	// peers are pruned and cannot serve block 1.
	Peers string
}

// mochaPeers are persistent peers for mocha-5 that retain the full block
// history, so a node block syncing from genesis can fetch block 1. Most
// public mocha peers are pruned and cannot serve it.
var mochaPeers = strings.Join([]string{
	"037dec108882cf0b291d3f4677246c8b0b68a356@149.50.96.5:12056",
	"53849d640006381d7bdca4d0de9506596c8d59af@188.40.66.173:11656",
	"5de6dcfb9ab6c4d8882596b2ad42cef2b67afc76@139.84.244.61:26656",
	"73e3f071fc2246608642a675bd9ecaee5a489641@216.152.153.85:26656",
	"7bfeb36f013e1030f0e8e2987540a86d2576e90a@69.72.83.18:27656",
	"b402fe40f3474e9e208840702e1b7aa37f2edc4b@65.109.69.119:14656",
	"b44257612a1546750e8d7a12e4df6e3771f3542a@216.106.185.180:11656",
	"c6247d57a922d070dabbbbc0d4d470e1ee7683df@14.6.2.60:26656",
	"d5519e378247dfb61dfe90652d1fe3e2b3005a5b@176.9.127.54:12056",
	"daf2cecee2bd7f1b3bf94839f993f807c6b15fbf@celestia-testnet-peer.itrocket.net:11656",
	"dbd78d7c61f1789814685d5ed37fb39ff054177d@135.181.227.236:39656",
	"ea9994ae9cd191cff268a885580cd58562000c41@203.209.219.89:656",
	"ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@84.32.215.148:26656",
}, ",")

// NewMochaConfig returns a Config for the mocha testnet
func NewMochaConfig() *Config {
	return &Config{
		Name:    "mocha",
		ChainID: appconsts.MochaChainID,
		// State sync requires >= 2 RPC servers to cross-verify the app hash
		// header. These must be distinct providers: listing one host twice
		// gives no redundancy, so a single slow/unavailable provider stalls
		// state sync. Keep these in sync with the live mocha-5 testnet.
		RPCs: []string{
			"https://rpc-mocha.pops.one:443",
			"https://celestia-testnet-rpc.itrocket.net:443",
		},
		// seeds provide dynamic peer discovery — the node contacts a seed,
		// gets a fresh list of currently-alive peers, and connects. This is
		// more resilient than hardcoded persistent peers which go stale.
		// Keep in sync with https://github.com/celestiaorg/networks/blob/main/mocha-5/seeds.txt
		Seeds: "ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@84.32.215.148:26656,b402fe40f3474e9e208840702e1b7aa37f2edc4b@celestia-testnet-seed.itrocket.net:14656",
		// Archive nodes (earliest_block_height = 1 on their RPC /status).
		Peers: mochaPeers,
	}
}

// NewCortoConfig returns a Config for the Corto internal testnet. Corto has
// no public endpoints, so the RPC and gRPC endpoints must be provided via the
// CORTO_RPC and CORTO_GRPC env vars. If the endpoints require authentication,
// the token is provided via the optional CORTO_AUTH_TOKEN env var.
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
		Name:      "corto",
		ChainID:   appconsts.CortoChainID,
		RPCs:      []string{rpc},
		GRPCs:     []string{grpc},
		AuthToken: os.Getenv("CORTO_AUTH_TOKEN"),
	}, nil
}

// NewMainnetConfig returns a Config for Mainnet Beta.
func NewMainnetConfig() *Config {
	return &Config{
		Name:    "mainnet",
		ChainID: appconsts.MainnetChainID,
		// Distinct archive RPC providers. Keep in sync with the live network.
		RPCs: []string{
			"https://rpc.celestia.pops.one:443",
			"https://celestia-mainnet-rpc.itrocket.net:443",
		},
		// Keep in sync with https://github.com/celestiaorg/networks/blob/master/celestia/seeds.txt
		Seeds: "acca7837e4eb5f9dc7f5a94ed1d82edda6931ff8@seed.celestia.pops.one:26656,12ad7c73c7e1f2460941326937a039139aa78884@celestia-mainnet-seed.itrocket.net:40656,9b1d22c3a78487d1a664a4b6a331fce527d14fb4@seed.celestia.mainnet.dteam.tech:27656",
		// Archive nodes (earliest_block_height = 1 on their RPC /status).
		Peers: "acca7837e4eb5f9dc7f5a94ed1d82edda6931ff8@seed.celestia.pops.one:26656,d535cbf8d0efd9100649aa3f53cb5cbab33ef2d6@celestia-mainnet-peer.itrocket.net:26656,ff016c95251136c729a1f6e60d6dda6db0aeb666@195.154.218.184:26656,e0c570997f9c4ecc3b1c4c0f5fbb2681031e5353@157.180.10.38:40656",
	}
}
