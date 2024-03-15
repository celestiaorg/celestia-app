package interchaintest_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	chantypes "github.com/cosmos/ibc-go/v6/modules/core/04-channel/types"
	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"
	"github.com/strangelove-ventures/interchaintest/v6/relayer"
	"github.com/strangelove-ventures/interchaintest/v6/testreporter"
	"github.com/strangelove-ventures/interchaintest/v6/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const relayerName = "relayer"
const path = "test-path"

// TestICA verifies that Interchain Accounts work as expected.
func TestICA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestICA in short mode.")
	}

	client, network := interchaintest.DockerSetup(t)
	celestia, gaia := getChains(t)

	relayer := interchaintest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t),
		relayer.RelayerOptionExtraStartFlags{Flags: []string{"-p", "events", "-b", "100"}},
	).Build(t, client, network)

	ic := interchaintest.NewInterchain().
		AddChain(celestia).
		AddChain(gaia).
		AddRelayer(relayer, relayerName).
		AddLink(interchaintest.InterchainLink{
			Chain1:  celestia,
			Chain2:  gaia,
			Relayer: relayer,
			Path:    path,
		})

	ctx := context.Background()
	reporter := testreporter.NewNopReporter().RelayerExecReporter(t)
	err := ic.Build(ctx, reporter, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ic.Close() })

	err = relayer.GeneratePath(ctx, reporter, celestia.Config().ChainID, gaia.Config().ChainID, path)
	require.NoError(t, err)

	err = relayer.CreateClients(ctx, reporter, path, ibc.CreateClientOptions{TrustingPeriod: "330h"})
	require.NoError(t, err)

	err = testutil.WaitForBlocks(ctx, 2, celestia, gaia)
	require.NoError(t, err)

	err = relayer.CreateConnections(ctx, reporter, path)
	require.NoError(t, err)

	connections, err := relayer.GetConnections(ctx, reporter, celestia.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, connections, 1)

	connections, err = relayer.GetConnections(ctx, reporter, gaia.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, connections, 1)

	err = relayer.StartRelayer(ctx, reporter, path)
	require.NoError(t, err)

	amount := math.NewIntFromUint64(uint64(10_000_000_000))
	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), amount, celestia, gaia)
	// celestiaUser := users[0]
	gaiaUser := users[1]
	gaiaAddr := gaiaUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(gaia.Config().Bech32Prefix)

	registerICA := []string{
		gaia.Config().Bin, "tx", "intertx", "register",
		"--from", gaiaAddr,
		"--connection-id", connections[0].ID,
		"--chain-id", gaia.Config().ChainID,
		"--home", gaia.HomeDir(),
		"--node", gaia.GetRPCAddress(),
		"--keyring-backend", keyring.BackendTest,
		"-y",
	}
	_, _, err = gaia.Exec(ctx, registerICA, nil)
	require.NoError(t, err)

	celestiaHeight, err := celestia.Height(ctx)
	require.NoError(t, err)

	isChannelOpen := func(found *chantypes.MsgChannelOpenConfirm) bool {
		return found.PortId == "icahost"
	}
	_, err = cosmos.PollForMessage(ctx, celestia, cosmos.DefaultEncoding().InterfaceRegistry, celestiaHeight, celestiaHeight+30, isChannelOpen)
	require.NoError(t, err)

	// Query for the newly registered interchain account
	queryICA := []string{
		gaia.Config().Bin, "query", "intertx", "interchainaccounts", connections[0].ID, gaiaAddr,
		"--chain-id", gaia.Config().ChainID,
		"--home", gaia.HomeDir(),
		"--node", gaia.GetRPCAddress(),
	}
	stdout, _, err := gaia.Exec(ctx, queryICA, nil)
	require.NoError(t, err)
	t.Log(stdout)
}
