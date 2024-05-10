package interchain

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/test/interchain/chainspec"
	transfertypes "github.com/cosmos/ibc-go/v6/modules/apps/transfer/types"
	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"
	"github.com/strangelove-ventures/interchaintest/v6/testreporter"
	"github.com/strangelove-ventures/interchaintest/v6/testutil"
	"github.com/stretchr/testify/require"
)

type PacketMetadata struct {
	Forward *ForwardMetadata `json:"forward"`
}

type ForwardMetadata struct {
	Receiver       string        `json:"receiver"`
	Port           string        `json:"port"`
	Channel        string        `json:"channel"`
	Timeout        time.Duration `json:"timeout"`
	Retries        *uint8        `json:"retries,omitempty"`
	Next           *string       `json:"next,omitempty"`
	RefundSequence *uint64       `json:"refund_sequence,omitempty"`
}

// TestPacketForwardMiddleware verifies that Packet Forward Middleware (PFM) works as expected by sending a transfer from Celestia to Cosmos Hub, back to Celestia which then forwards it to Cosmos Hub 2. Celestia -> CosmosHub -> Celestia -> CosmosHub2 .
func TestPacketForwardMiddleware(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestPacketForwardMiddleware in short mode.")
	}

	client, network := interchaintest.DockerSetup(t)
	celestia := chainspec.GetCelestia(t)
	cosmosHub := chainspec.GetCosmosHub(t)
	cosmosHub2 := chainspec.GetCosmosHub2(t)
	relayer := getRelayerFactory(t).Build(t, client, network)

	// set up paths for the chains
	pathCosmosHubCelestia := fmt.Sprintf("%s%s", cosmosHub.Config().ChainID, celestia.Config().ChainID)
	pathCelestiaCosmosHub2 := fmt.Sprintf("%s%s", celestia.Config().ChainID, cosmosHub2.Config().ChainID)

	interchain := interchaintest.NewInterchain().
		AddChain(cosmosHub).
		AddChain(celestia).
		AddChain(cosmosHub2).
		AddRelayer(relayer, getRelayerName()).
		AddLink(interchaintest.InterchainLink{
			Chain1:  cosmosHub,
			Chain2:  celestia,
			Relayer: relayer,
			Path:    pathCosmosHubCelestia,
		}).
		AddLink(interchaintest.InterchainLink{
			Chain1:  celestia,
			Chain2:  cosmosHub2,
			Relayer: relayer,
			Path:    pathCelestiaCosmosHub2,
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

	err = relayer.StartRelayer(ctx, reporter, pathCosmosHubCelestia, pathCelestiaCosmosHub2)
	require.NoError(t, err)

	err = testutil.WaitForBlocks(ctx, 2, celestia, cosmosHub)
	require.NoError(t, err)

	celestiaConnections, err := relayer.GetConnections(ctx, reporter, celestia.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, celestiaConnections, 2)

	cosmosHubConnections, err := relayer.GetConnections(ctx, reporter, cosmosHub.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, cosmosHubConnections, 2) // 2 connections: the first is connection-0 and the second is connection-localhost.

	// TODO check what these two connections are
	cosmosHub2Connections, err := relayer.GetConnections(ctx, reporter, cosmosHub2.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, cosmosHub2Connections, 2) // 2 connections: the first is connection-0 and the second is connection-localhost.

	// fund the users
	initialBalance := math.NewInt(10_000_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), initialBalance, celestia, cosmosHub, cosmosHub2)
	err = testutil.WaitForBlocks(ctx, 2, celestia, cosmosHub, cosmosHub2)
	require.NoError(t, err)

	// get the addresses of the users on the 3 chains
	celestiaUser, cosmosUser, cosmosUser2 := users[0], users[1], users[2]
	celestiaAddr := celestiaUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(celestia.Config().Bech32Prefix)
	cosmosAddr := cosmosUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(cosmosHub.Config().Bech32Prefix)
	cosmosAddr2 := cosmosUser2.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(cosmosHub2.Config().Bech32Prefix)
	fmt.Printf("celestiaAddr: %s, cosmosAddr: %v, cosmosAddr2: %v\n", celestiaAddr, cosmosAddr, cosmosAddr2)

	// create channels
	cosmosHubCelestiaChan, err := ibc.GetTransferChannel(ctx, relayer, reporter, cosmosHub.Config().ChainID, celestia.Config().ChainID)
	require.NoError(t, err)
	celestiaCosmosHub2Chan, err := ibc.GetTransferChannel(ctx, relayer, reporter, celestia.Config().ChainID, cosmosHub2.Config().ChainID)
	require.NoError(t, err)

	transferAmount := math.NewInt(100_000)

	firstHopDenom := transfertypes.GetPrefixedDenom(cosmosHubCelestiaChan.PortID, cosmosHubCelestiaChan.ChannelID, celestia.Config().Denom)
	secondHopDenom := transfertypes.GetPrefixedDenom(celestiaCosmosHub2Chan.PortID, celestiaCosmosHub2Chan.ChannelID, celestia.Config().Denom)

	firstHopDenomTrace := transfertypes.ParseDenomTrace(firstHopDenom)
	secondHopDenomTrace := transfertypes.ParseDenomTrace(secondHopDenom)
	fmt.Println(secondHopDenomTrace, "SECOND HOP DENOM TRACE")

	// first send some tia to cosmos
	transfer := ibc.WalletAmount{
		Address: cosmosAddr,
		Denom:   celestia.Config().Denom,
		Amount:  transferAmount,
	}

	// celestia should forward the packet to cosmos2
	secondHopMetadata := &PacketMetadata{
		Forward: &ForwardMetadata{
			Receiver: cosmosAddr2,
			Channel:  celestiaCosmosHub2Chan.ChannelID,
			Port:     celestiaCosmosHub2Chan.PortID,
		},
	}
	nextBz, err := json.Marshal(secondHopMetadata)
	require.NoError(t, err)
	next := string(nextBz)

	// send tia back to celestia for it to forward to cosmos2
	firstHopMetadata := &PacketMetadata{
		Forward: &ForwardMetadata{
			Receiver: celestiaAddr,
			Channel:  cosmosHubCelestiaChan.ChannelID,
			Port:     cosmosHubCelestiaChan.PortID,
			Next:     &next,
		},
	}

	memo, err := json.Marshal(firstHopMetadata)
	require.NoError(t, err)

	celHeight, err := celestia.Height(ctx)
	require.NoError(t, err)

	transferTx, err := celestia.SendIBCTransfer(ctx, cosmosHubCelestiaChan.ChannelID, celestiaUser.KeyName(), transfer, ibc.TransferOptions{Memo: string(memo)})
	require.NoError(t, err)

	_, err = testutil.PollForAck(ctx, celestia, celHeight, celHeight+30, transferTx.Packet)
	require.NoError(t, err)
	err = testutil.WaitForBlocks(ctx, 3, celestia)
	require.NoError(t, err)

	queryCelestiaBal := []string{
		celestia.Config().Bin, "query", "bank", "balances", celestiaAddr,
		"--chain-id", celestia.Config().ChainID,
		"--home", celestia.HomeDir(),
		"--node", celestia.GetRPCAddress(),
	}
	stdout, stderr, err := celestia.Exec(ctx, queryCelestiaBal, nil)
	require.NoError(t, err)
	require.Empty(t, stderr)
	fmt.Println(string(stdout), "STDOUT")
	t.Logf("stdout %v\n", string(stdout))

	cosmosUserBalance, err := cosmosHub.GetBalance(ctx, cosmosAddr, firstHopDenomTrace.IBCDenom())
	require.NoError(t, err)
	require.True(t, cosmosUserBalance.Equal(math.NewInt(0)))
	fmt.Println(cosmosUserBalance, "COSMOS BALANCE")

	cosmosUser2Balance, err := cosmosHub2.GetBalance(ctx, cosmosAddr2, firstHopDenomTrace.IBCDenom())
	require.NoError(t, err)
	// factoring in the network fee on celestia
	// celestiaNetworkFee := 0.002
	// require.True(t, cosmosBalance.Equal(initialBalance.Sub(transferAmount).Sub(math.NewInt(int64(celestiaNetworkFee)))))
	fmt.Println(cosmosUser2Balance, "COSMOS 2 BALANCE")
}
