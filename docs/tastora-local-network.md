# Running a Local Custom Network with Tastora

This guide explains how to use [Tastora](https://github.com/celestiaorg/tastora) to run a local custom Celestia network with Docker. This is useful for testing custom modifications or conducting bug bounty research.

## Prerequisites

- Docker installed and running
- Go 1.24.6 or later

## Basic Example

```go
package mytests

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/test/docker-e2e/dockerchain"
	"github.com/celestiaorg/celestia-app/v6/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/stretchr/testify/require"
)

func TestCustomNetwork(t *testing.T) {
	ctx := context.Background()

	dockerClient, networkID := tastoradockertypes.DockerSetup(t)

	tnCfg := testnode.DefaultConfig()
	tnCfg.Genesis = tnCfg.Genesis.
		WithChainID("my-custom-chain").
		WithValidators(
			genesis.NewDefaultValidator("validator1"),
			genesis.NewDefaultValidator("validator2"),
		)

	cfg := dockerchain.DefaultConfig(dockerClient, networkID)
	cfg = cfg.WithConfig(tnCfg)

	chain, err := dockerchain.NewCelestiaChainBuilder(t, cfg).Build(ctx)
	require.NoError(t, err)

	err = chain.Start(ctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			t.Logf("Error removing chain: %v", err)
		}
	})

	networkInfo, err := chain.GetNodes()[0].GetNetworkInfo(ctx)
	require.NoError(t, err)

	t.Logf("RPC: http://%s", networkInfo.External.RPCAddress())
	t.Logf("gRPC: %s", networkInfo.External.GRPCAddress())
	t.Logf("API: http://%s", networkInfo.External.APIAddress())
}
```

## Using Test Suite Infrastructure

For more complex tests, use the test suite pattern from `test/docker-e2e/e2e_test.go`:

```go
package docker_e2e

import (
	"context"
	"testing"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"github.com/celestiaorg/celestia-app/v6/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/suite"
)

func TestMyCustomNetwork(t *testing.T) {
	suite.Run(t, new(MyCustomTestSuite))
}

type MyCustomTestSuite struct {
	suite.Suite
	client  *client.Client
	network string
}

func (s *MyCustomTestSuite) SetupSuite() {
	s.client, s.network = tastoradockertypes.DockerSetup(s.T())
}

func (s *MyCustomTestSuite) TestCustomChain() {
	ctx := context.Background()

	tnCfg := testnode.DefaultConfig()
	tnCfg.Genesis = tnCfg.Genesis.
		WithChainID("my-test-chain").
		WithValidators(
			genesis.NewDefaultValidator("validator1"),
		)

	cfg := dockerchain.DefaultConfig(s.client, s.network)
	cfg = cfg.WithConfig(tnCfg)

	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
	s.Require().NoError(err)

	err = chain.Start(ctx)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			s.T().Logf("Error removing chain: %v", err)
		}
	})
}
```

## Customizing Network Parameters

### Custom Genesis Parameters

Customize validators and chain ID:

```go
tnCfg.Genesis = tnCfg.Genesis.
	WithChainID("my-chain").
	WithValidators(
		genesis.NewDefaultValidator("validator1"),
		genesis.NewDefaultValidator("validator2"),
		genesis.NewDefaultValidator("validator3"),
	)
```

### Custom Docker Image

Use a custom Docker image:

```go
cfg = cfg.
	WithImage("my-custom-image").
	WithTag("my-tag")
```

Or set environment variables:
```bash
export CELESTIA_IMAGE="my-custom-image"
export CELESTIA_TAG="my-tag"
```

### Custom Node Configuration

See `test/docker-e2e/dockerchain/testchain.go` for examples of post-initialization modifications that customize block time, gas prices, and network settings.

## Submitting Transactions

Submit transactions using the TxClient:

```go
import (
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/celestia-app/v6/test/docker-e2e/dockerchain"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

txClient, err := dockerchain.SetupTxClient(ctx, chain.Nodes()[0], cfg)
require.NoError(t, err)

msg := banktypes.NewMsgSend(fromAddr, toAddr, sendAmount)
txResp, err := txClient.SubmitTx(ctx, []sdk.Msg{msg}, 
	user.SetGasLimit(200000), 
	user.SetFee(5000),
)
require.NoError(t, err)
```

See `test/docker-e2e/e2e_simple_test.go` for complete examples.

## Testing Custom Modifications

Build a custom Docker image with your modifications:

```bash
docker build -t my-custom-celestia-app:latest .
```

Then use it in your test:

```go
cfg = cfg.
	WithImage("my-custom-celestia-app").
	WithTag("latest")
```

## Troubleshooting

- Ensure Docker is running: `docker info`
- Default image tag is `v5.0.10` (see `test/docker-e2e/dockerchain/config.go`)
- Port conflicts are handled automatically by Tastora

## Additional Resources

- [Tastora Documentation](https://github.com/celestiaorg/tastora)
- Example tests: `test/docker-e2e/`
- Chain configuration utilities: `test/docker-e2e/dockerchain/`

