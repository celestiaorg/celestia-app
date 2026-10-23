package networks

import (
	"fmt"
	"os"
	"strings"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
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
	// peers are pruned and cannot serve block 1. They are a fallback for
	// FetchArchivePeers, which reads the live addresses off the RPCs.
	Peers string
}

// mochaPeers are the fallback archive peers for mocha-5, used when the RPCs
// cannot be reached. Most public mocha-5 peers are pruned to roughly the last
// 3000 blocks, so keep at least two independent operators here: with a single
// archive peer, the genesis sync test fails whenever that one node ignores our
// dials. Do not grow this list with peers whose RPC /status reports
// earliest_block_height = 1 without checking what the p2p address itself
// serves — a node can expose the full history over RPC and still run in seed
// mode, which disconnects before sending a block.
var mochaPeers = strings.Join([]string{
	"daf2cecee2bd7f1b3bf94839f993f807c6b15fbf@celestia-testnet-peer.itrocket.net:26656",
	// Celestia's own archival consensus node, which also serves rpc.celestia-mocha.com.
	"ed8359cd707c622b0a9250824e311ee1fdb65ba0@consensus.celestia-mocha.com:26656",
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
		// The Celestia-operated archive RPC comes first because the
		// third-party providers rate-limit CI runners, and a third provider
		// keeps one witness alive when another returns 429. It is also an
		// archival node, so FetchArchivePeers finds a second archive peer
		// besides itrocket.
		RPCs: []string{
			"https://rpc.celestia-mocha.com:443",
			"https://rpc-mocha.pops.one:443",
			"https://celestia-testnet-rpc.itrocket.net:443",
		},
		// seeds provide dynamic peer discovery — the node contacts a seed,
		// gets a fresh list of currently-alive peers, and connects. This is
		// more resilient than hardcoded persistent peers which go stale.
		// Keep in sync with https://github.com/celestiaorg/networks/blob/main/mocha-5/seeds.txt
		Seeds: "ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@84.32.215.148:26656,b402fe40f3474e9e208840702e1b7aa37f2edc4b@celestia-testnet-seed.itrocket.net:14656",
		// Fallback archive peers; FetchArchivePeers is preferred.
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
		// Fallback archive peers; FetchArchivePeers is preferred.
		Peers: mainnetPeers,
	}
}

// mainnetPeers are archive nodes (earliest_block_height = 1). Public mainnet
// nodes usually have every inbound slot taken, and a node with full slots
// accepts the TCP connection but never completes the handshake. Each extra
// archive peer is another chance that one has a free slot during the test.
// Seeds stay out of this list even when they keep the full history: they
// disconnect right after the address exchange and never serve a block, so as
// persistent peers they only cause a reconnect loop.
var mainnetPeers = strings.Join([]string{
	"d535cbf8d0efd9100649aa3f53cb5cbab33ef2d6@celestia-mainnet-peer.itrocket.net:26656",
	"e0c570997f9c4ecc3b1c4c0f5fbb2681031e5353@157.180.10.38:40656", // LithiumDigital archive
	// Celestia DA-proxy archival backends (pruning = "nothing", min-retain-blocks = 0).
	"ff016c95251136c729a1f6e60d6dda6db0aeb666@195.154.218.184:26656", // da-proxy-rpc-2-mainnet
	"e8a2bc3681108f862371286094d90018487edeb7@195.154.103.170:26656", // da-proxy-rpc-1-mainnet
}, ",")
