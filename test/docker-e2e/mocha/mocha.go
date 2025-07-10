package mocha

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"fmt"
	"github.com/celestiaorg/celestia-app/v5/app"
	"github.com/celestiaorg/celestia-app/v5/test/util/testnode"
	celestiadockertypes "github.com/celestiaorg/tastora/framework/docker"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"testing"
)

const (
	ChainID = "mocha-4"
	RPC1    = "https://celestia-testnet-rpc.itrocket.net:443"
	RPC2    = "https://celestia-mocha-rpc.publicnode.com:443"
	Seeds   = "5d0bf034d6e6a8b5ee31a2f42f753f1107b3a00e@celestia-testnet-seed.itrocket.net:11656"
)

// NewConfig returns a configured instance of dockerchain.Config for the mocha testnet.
func NewConfig(client *client.Client, network string) (*dockerchain.Config, error) {
	// create minimal config - the genesis will be downloaded by the NewChainBuilder
	tnCfg := testnode.DefaultConfig()
	tnCfg.Genesis = tnCfg.Genesis.WithChainID(ChainID)

	cfg := &dockerchain.Config{}
	return cfg.
		WithConfig(tnCfg).
		WithImage("ghcr.io/celestiaorg/celestia-app").
		WithTag("v4.0.6-mocha").
		WithDockerClient(client).
		WithDockerNetworkID(network), nil
}

// NewChainBuilder constructs a new ChainBuilder configured for connecting to the mocha testnet.
func NewChainBuilder(t *testing.T, cfg *dockerchain.Config) *celestiadockertypes.ChainBuilder {
	// download mocha genesis
	genesisBz, err := downloadGenesis(ChainID)
	require.NoError(t, err, "failed to download mocha genesis")

	encodingConfig := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)

	return celestiadockertypes.NewChainBuilder(t).
		WithName("mocha-state-sync").
		WithChainID(ChainID).
		WithDockerClient(cfg.DockerClient).
		WithDockerNetworkID(cfg.DockerNetworkID).
		WithImage(celestiadockertypes.NewDockerImage(cfg.Image, cfg.Tag, "10001:10001")).
		WithAdditionalStartArgs("--force-no-bbr").
		WithEncodingConfig(&encodingConfig).
		WithGenesis(genesisBz)
}

// NewClient creates a new RPC client for connecting to the mocha network.
func NewClient() (*rpchttp.HTTP, error) {
	return rpchttp.New(RPC1, "/websocket")
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
