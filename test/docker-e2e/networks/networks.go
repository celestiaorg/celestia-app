package networks

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	celestiadockertypes "github.com/celestiaorg/tastora/framework/docker"
	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// NewConfig returns a configured instance of dockerchain.Config for the specified chain.
func NewConfig(networkCfg *Config, client *client.Client, network string) (*dockerchain.Config, error) {
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
