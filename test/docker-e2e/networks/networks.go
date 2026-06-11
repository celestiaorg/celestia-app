package networks

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	tastoratypes "github.com/celestiaorg/tastora/framework/types"

	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/test/util/testnode"
	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	celestiadockertypes "github.com/celestiaorg/tastora/framework/docker/cosmos"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/stretchr/testify/require"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
)

// NewConfig returns a configured instance of dockerchain.Config for the specified chain.
func NewConfig(networkCfg *Config, client tastoratypes.TastoraDockerClient, network string) (*dockerchain.Config, error) {
	// create minimal config - the genesis will be downloaded by the NewChainBuilder
	tnCfg := testnode.DefaultConfig()
	tnCfg.Genesis = tnCfg.Genesis.WithChainID(networkCfg.ChainID)

	cfg := &dockerchain.Config{}
	return cfg.
		WithConfig(tnCfg).
		WithImage(dockerchain.GetCelestiaImage()).
		WithTag(dockerchain.GetCelestiaTag()).
		WithDockerClient(client).
		WithDockerNetworkID(network), nil
}

// NewChainBuilder constructs a new ChainBuilder configured for connecting to the specified live chain.
func NewChainBuilder(t *testing.T, chainConfig *Config, cfg *dockerchain.Config) *celestiadockertypes.ChainBuilder {
	// download genesis for the specified chain
	genesisBz, err := downloadGenesis(chainConfig.ChainID)
	require.NoError(t, err, "failed to download %s genesis", chainConfig.Name)

	encodingConfig := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)

	return celestiadockertypes.NewChainBuilder(t).
		WithName(chainConfig.Name).
		WithChainID(chainConfig.ChainID).
		WithDockerClient(cfg.DockerClient).
		WithDockerNetworkID(cfg.DockerNetworkID).
		WithImage(tastoracontainertypes.NewImage(cfg.Image, cfg.Tag, "10001:10001")).
		WithAdditionalStartArgs("--force-no-bbr").
		WithEncodingConfig(&encodingConfig).
		WithGenesis(genesisBz)
}

// NewClient creates a new RPC client for connecting to the specified chain.
func NewClient(rpc string) (*rpchttp.HTTP, error) {
	return rpchttp.New(rpc, "/websocket")
}

// FetchPeers queries /net_info on each RPC endpoint and returns a
// deduplicated, comma-separated list of peers suitable for
// --p2p.persistent_peers. It tries every RPC in the config and merges results
// so that transient failures on a single provider don't leave us with zero
// peers. maxPeers caps the returned list to avoid bloating the arg.
func FetchPeers(t *testing.T, rpcs []string, maxPeers int) string {
	t.Helper()

	seen := make(map[string]struct{})
	var peers []string

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, rpc := range rpcs {
		if len(peers) >= maxPeers {
			break
		}
		client, err := NewClient(rpc)
		if err != nil {
			t.Logf("FetchPeers: %s: %v", rpc, err)
			continue
		}
		netInfo, err := client.NetInfo(ctx)
		if err != nil {
			t.Logf("FetchPeers: %s: %v", rpc, err)
			continue
		}
		for _, p := range netInfo.Peers {
			id := string(p.NodeInfo.DefaultNodeID)
			if id == "" || p.RemoteIP == "" {
				continue
			}
			// skip IPv6 peers — CometBFT's persistent_peers format
			// doesn't reliably handle IPv6 addresses.
			if net.ParseIP(p.RemoteIP) != nil && strings.Contains(p.RemoteIP, ":") {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			// extract port from listen_addr (e.g. "tcp://0.0.0.0:26656")
			port := "26656"
			if parts := strings.Split(p.NodeInfo.ListenAddr, ":"); len(parts) > 1 {
				port = parts[len(parts)-1]
			}
			peers = append(peers, fmt.Sprintf("%s@%s:%s", id, p.RemoteIP, port))
		}
	}

	if len(peers) > maxPeers {
		peers = peers[:maxPeers]
	}
	t.Logf("FetchPeers: discovered %d peers from %d RPCs", len(peers), len(rpcs))
	return strings.Join(peers, ",")
}

// downloadGenesis downloads the genesis file for the given chain ID from the celestia networks repo
func downloadGenesis(chainID string) ([]byte, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/celestiaorg/networks/master/%s/genesis.json", chainID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download genesis file: HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
