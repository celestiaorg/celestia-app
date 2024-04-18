package interchain

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/test/interchain/chainspec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v6/testreporter"
	"github.com/strangelove-ventures/interchaintest/v6/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInterChainAccounts verifies that Inter-Chain Accounts (ICA) work as expected by creating
// an ICA on Celestia (host chain) using the Cosmos Hub (controller chain).
func TestInterChainAccounts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestInterChainAccounts in short mode.")
	}

	client, network := interchaintest.DockerSetup(t)
	celestia := chainspec.GetCelestia(t)
	cosmosHub := chainspec.GetCosmosHub(t)
	relayer := getRelayerFactory(t).Build(t, client, network)
	pathName := fmt.Sprintf("%s-to-%s", celestia.Config().ChainID, cosmosHub.Config().ChainID)
	interchain := interchaintest.NewInterchain().
		AddChain(celestia).
		AddChain(cosmosHub).
		AddRelayer(relayer, getRelayerName()).
		AddLink(interchaintest.InterchainLink{
			Chain1:  celestia,
			Chain2:  cosmosHub,
			Relayer: relayer,
			Path:    pathName,
		})

	ctx := context.Background()
	reporter := testreporter.NewNopReporter().RelayerExecReporter(t)
	err := interchain.Build(ctx, reporter, interchaintest.InterchainBuildOptions{
		TestName:  t.Name(),
		Client:    client,
		NetworkID: network,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = interchain.Close() })

	err = relayer.StartRelayer(ctx, reporter, pathName)
	require.NoError(t, err)

	err = testutil.WaitForBlocks(ctx, 2, celestia, cosmosHub)
	require.NoError(t, err)

	celestiaConnections, err := relayer.GetConnections(ctx, reporter, celestia.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, celestiaConnections, 1)

	cosmosConnections, err := relayer.GetConnections(ctx, reporter, cosmosHub.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, cosmosConnections, 2) // 2 connections: the first is connection-0 and the second is connection-localhost.
	cosmosConnection := cosmosConnections[0]

	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), math.NewInt(10_000_000_000), celestia, cosmosHub)
	err = testutil.WaitForBlocks(ctx, 2, celestia, cosmosHub)
	require.NoError(t, err)

	celestiaUser, cosmosUser := users[0], users[1]
	celestiaAddr := celestiaUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(celestia.Config().Bech32Prefix)
	cosmosAddr := cosmosUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(cosmosHub.Config().Bech32Prefix)
	fmt.Printf("celestiaAddr: %s, cosmosAddr: %v\n", celestiaAddr, cosmosAddr)

	registerICA := []string{
		cosmosHub.Config().Bin, "tx", "interchain-accounts", "controller", "register", cosmosConnection.ID,
		"--chain-id", cosmosHub.Config().ChainID,
		"--home", cosmosHub.HomeDir(),
		"--node", cosmosHub.GetRPCAddress(),
		"--from", cosmosUser.KeyName(),
		"--keyring-backend", keyring.BackendTest,
		"--fees", fmt.Sprintf("300000%v", cosmosHub.Config().Denom),
		"--gas", "300000", // the auto gas estimation underestimates the gas required.
		"--yes",
	}
	stdout, stderr, err := cosmosHub.Exec(ctx, registerICA, nil)
	require.NoError(t, err)
	require.Empty(t, stderr)
	t.Logf("stdout %v", string(stdout))

	err = testutil.WaitForBlocks(ctx, 5, celestia, cosmosHub)
	require.NoError(t, err)

	queryICA := []string{
		cosmosHub.Config().Bin, "query", "interchain-accounts", "controller", "interchain-account", cosmosAddr, cosmosConnection.ID,
		"--chain-id", cosmosHub.Config().ChainID,
		"--home", cosmosHub.HomeDir(),
		"--node", cosmosHub.GetRPCAddress(),
	}
	stdout, stderr, err = cosmosHub.Exec(ctx, queryICA, nil)
	require.NoError(t, err)
	require.Empty(t, stderr)
	t.Logf("stdout %v\n", string(stdout))
	assert.NotEmpty(t, string(stdout))

	channels, err := relayer.GetChannels(ctx, reporter, cosmosHub.Config().ChainID)
	require.NoError(t, err)
	fmt.Printf("cosmosHub channels: %#v\n", channels)
	assert.Len(t, channels, 2) // 2 channels: the first is ics27-1, the second is ics20-1.
	assert.True(t, strings.HasPrefix(channels[0].PortID, "icacontroller"), "Expected %v to start with %v", channels[0].PortID, "icacontroller")

	channels, err = relayer.GetChannels(ctx, reporter, celestia.Config().ChainID)
	require.NoError(t, err)
	fmt.Printf("celestia channels: %#v\n", channels)
	assert.Len(t, channels, 2) // 2 channels: the first is ics27-1, the second is ics20-1.
	assert.Equal(t, "icahost", channels[0].PortID)
}
