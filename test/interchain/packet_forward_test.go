package interchain

import (
	"context"
	"fmt"
	// "strings"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/test/interchain/chainspec"
	// "github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v6/testreporter"
	"github.com/strangelove-ventures/interchaintest/v6/testutil"
	"github.com/stretchr/testify/assert"
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

// TestPacketForwardMiddleware verifies that Packet Forward Middleware (PFM) works as expected by sending a transfer from chain A to chain C through chain B Celestia tht has .
func TestPacketForwardMiddleware(t *testing.T) {
	// if testing.Short() {
		t.Skip("skipping TestPacketForwardMiddleware in short mode.")
	// }

	client, network := interchaintest.DockerSetup(t)
	celestia := chainspec.GetCelestia(t)
	cosmosHub := chainspec.GetCosmosHub(t)
	cosmosHub2 := chainspec.GetCosmosHub2(t)
	relayer := getRelayerFactory(t).Build(t, client, network)

	// NINA: SETTING UP PATHS BETWEEN THE 3 CHAINS
	pathCosmosCelestia := fmt.Sprintf("%s-to-%s", cosmosHub.Config().ChainID, celestia.Config().ChainID)
	pathCelestiaCosmos2 := fmt.Sprintf("%s-to-%s", celestia.Config().ChainID, cosmosHub2.Config().ChainID)

	interchain := interchaintest.NewInterchain().
		AddChain(cosmosHub).
		AddChain(celestia).
		AddChain(cosmosHub2).
		AddRelayer(relayer, getRelayerName()).
		AddLink(interchaintest.InterchainLink{
			Chain1:  cosmosHub,
			Chain2:  celestia,
			Relayer: relayer,
			Path:    pathCosmosCelestia,
		}).
		AddLink(interchaintest.InterchainLink{
			Chain1:  celestia,
			Chain2:  cosmosHub2,
			Relayer: relayer,
			Path:    pathCelestiaCosmos2,
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

	err = relayer.StartRelayer(ctx, reporter, pathCosmosCelestia, pathCelestiaCosmos2)
	require.NoError(t, err)

	err = testutil.WaitForBlocks(ctx, 2, celestia, cosmosHub)
	require.NoError(t, err)

	celestiaConnections, err := relayer.GetConnections(ctx, reporter, celestia.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, celestiaConnections, 1)

	cosmosConnections, err := relayer.GetConnections(ctx, reporter, cosmosHub.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, cosmosConnections, 2) // 2 connections: the first is connection-0 and the second is connection-localhost.
	// cosmosConnection := cosmosConnections[0]

	// NINA: GETTING USERS AND THEIR ADDRESSES TO USE IN THE TEST (done for all 3 chains!!)
	initialBalance := math.NewInt(10_000_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), initialBalance, celestia, cosmosHub, cosmosHub2)
	err = testutil.WaitForBlocks(ctx, 2, celestia, cosmosHub, cosmosHub2)
	require.NoError(t, err)

	// NINA: GETTING USERS AND THEIR ADDRESSES TO USE IN THE TEST (done for all 3 chains!!)
	celestiaUser, cosmosUser, cosmosUser2 := users[0], users[1], users[2]
	celestiaAddr := celestiaUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(celestia.Config().Bech32Prefix)
	cosmosAddr := cosmosUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(cosmosHub.Config().Bech32Prefix)
	cosmosAddr2 := cosmosUser2.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(cosmosHub2.Config().Bech32Prefix)
	fmt.Printf("celestiaAddr: %s, cosmosAddr: %v, cosmosAddr2: %v\n", celestiaAddr, cosmosAddr, cosmosAddr2)

	// NINA: GET USE BALANCES
	celestiaBalance, err := celestia.GetBalance(ctx, string(celestiaUser.Address()), celestia.Config().Denom)
	require.NoError(t, err)
	cosmosBalance, err := cosmosHub.GetBalance(ctx, string(cosmosUser.Address()), cosmosHub.Config().Denom)
	require.NoError(t, err)
	cosmosBalance2, err := cosmosHub2.GetBalance(ctx, string(cosmosUser2.Address()), cosmosHub2.Config().Denom)
	require.NoError(t, err)
    
	// verify initial balances
	assert.Equal(t, initialBalance, cosmosBalance)
	assert.Equal(t, math.NewInt(0), celestiaBalance)
	assert.Equal(t, math.NewInt(0), cosmosBalance2)
}
