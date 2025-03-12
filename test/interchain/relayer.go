package interchain

import (
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/relayer"
	"go.uber.org/zap/zaptest"
)

const (
	relayerDockerRepository = "ghcr.io/cosmos/relayer"
	relayerDockerVersion    = "v2.4.1"
	relayerUidGid           = "100:1000"
)

func getRelayerName() string {
	return "cosmos-relayer"
}

func getRelayerFactory(t *testing.T) interchaintest.RelayerFactory {
	return interchaintest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t),
		relayer.CustomDockerImage(relayerDockerRepository, relayerDockerVersion, relayerUidGid),
	)
}
