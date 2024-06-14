package interchain

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/test/interchain/chainspec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v6/modules/apps/transfer/types"
	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"
	"github.com/strangelove-ventures/interchaintest/v6/testreporter"
	"github.com/strangelove-ventures/interchaintest/v6/testutil"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
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

type QueryCelestiaBalResponse struct {
	Balances []struct {
		Amount string `yaml:"amount"`
		Denom  string `yaml:"denom"`
	} `yaml:"balances"`
}

// TestPacketForwardMiddleware verifies that Packet Forward Middleware (PFM) works as expected by sending a transfer from chain A to chain C through chain B Celestia tht has .
func TestPFM(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestPacketForwardMiddleware in short mode.")
	}

	client, network := interchaintest.DockerSetup(t)
	celestia := chainspec.GetCelestia(t)
	cosmosHub := chainspec.GetCosmosHub(t)
	cosmosHub2 := chainspec.GetCosmosHub2(t)
	relayer := getRelayerFactory(t).Build(t, client, network)

	// Set up paths for the chains
	pathCosmosHubCelestia := fmt.Sprintf("%s%s", cosmosHub.Config().ChainID, celestia.Config().ChainID)
	pathCelestiaCosmosHub2 := fmt.Sprintf("%s%s", celestia.Config().ChainID, cosmosHub2.Config().ChainID)
    
	// Create the interchain
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
 
	// Start the relayer
	err = relayer.StartRelayer(ctx, reporter, pathCosmosHubCelestia, pathCelestiaCosmosHub2)
	require.NoError(t, err)

	err = testutil.WaitForBlocks(ctx, 2, celestia, cosmosHub)
	require.NoError(t, err)
   
	// Get the connections
	celestiaConnections, err := relayer.GetConnections(ctx, reporter, celestia.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, celestiaConnections, 2)

	cosmosHubConnections, err := relayer.GetConnections(ctx, reporter, cosmosHub.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, cosmosHubConnections, 2)

	cosmosHub2Connections, err := relayer.GetConnections(ctx, reporter, cosmosHub2.Config().ChainID)
	require.NoError(t, err)
	require.Len(t, cosmosHub2Connections, 2)

	// Fund the users
	initialBalance := math.NewInt(10_000_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), initialBalance, celestia, cosmosHub, cosmosHub2)
	err = testutil.WaitForBlocks(ctx, 2, celestia, cosmosHub, cosmosHub2)
	require.NoError(t, err)

	// Get the user addresses
	celestiaUser, cosmosUser, cosmosUser2 := users[0], users[1], users[2]
	celestiaAddr := celestiaUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(celestia.Config().Bech32Prefix)
	cosmosAddr := cosmosUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(cosmosHub.Config().Bech32Prefix)
	cosmosAddr2 := cosmosUser2.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(cosmosHub2.Config().Bech32Prefix)
	fmt.Printf("celestiaAddr: %s, cosmosAddr: %v, cosmosAddr2: %v\n", celestiaAddr, cosmosAddr, cosmosAddr2)

	// Create the channels
	cosmosHubCelestiaChan, err := ibc.GetTransferChannel(ctx, relayer, reporter, cosmosHub.Config().ChainID, celestia.Config().ChainID)
	require.NoError(t, err)
	celestiaCosmosHub2Chan, err := ibc.GetTransferChannel(ctx, relayer, reporter, celestia.Config().ChainID, cosmosHub2.Config().ChainID)
	require.NoError(t, err)

	transferAmount := math.NewInt(100_000)

	firstHopDenom := transfertypes.GetPrefixedDenom(cosmosHubCelestiaChan.PortID, cosmosHubCelestiaChan.ChannelID, celestia.Config().Denom)

	firstHopDenomTrace := transfertypes.ParseDenomTrace(firstHopDenom)

	// Send some tia to cosmos
	transfer := ibc.WalletAmount{
		Address: cosmosAddr,
		Denom:   celestia.Config().Denom,
		Amount:  transferAmount,
	}

	// Celestia should forward the packet to cosmos2
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

	// Send tia back to celestia for it to forward to cosmos2
	firstHopMetadata := &PacketMetadata{
		Forward: &ForwardMetadata{
			Receiver: celestiaAddr,
			Channel:  cosmosHubCelestiaChan.ChannelID,
			Port:     cosmosHubCelestiaChan.PortID,
			Next:     &next,
		},
	}
	// Marshal the metadata
	memo, err := json.Marshal(firstHopMetadata)
	require.NoError(t, err)

	celHeight, err := celestia.Height(ctx)
	require.NoError(t, err)

	// Send the transfer
	transferTx, err := celestia.SendIBCTransfer(ctx, cosmosHubCelestiaChan.ChannelID, celestiaUser.KeyName(), transfer, ibc.TransferOptions{Memo: string(memo)})
	require.NoError(t, err)

	// Wait for the packet to be acknowledged
	_, err = testutil.PollForAck(ctx, celestia, celHeight, celHeight+30, transferTx.Packet)
	require.NoError(t, err)
	err = testutil.WaitForBlocks(ctx, 3, celestia, cosmosHub, cosmosHub2)
	require.NoError(t, err)

	// Query the celestia balance
	queryCelestiaBal := []string{
		celestia.Config().Bin, "query", "bank", "balances", celestiaAddr,
		"--chain-id", celestia.Config().ChainID,
		"--home", celestia.HomeDir(),
		"--node", celestia.GetRPCAddress(),
	}
	stdout, stderr, err := celestia.Exec(ctx, queryCelestiaBal, nil)
	require.NoError(t, err)
	require.Empty(t, stderr)

	// Parse the query response
	var response QueryCelestiaBalResponse
	err = yaml.Unmarshal(stdout, &response)
	require.NoError(t, err)

	// Cosmos user balance should be 0
	cosmosUserBalance, err := cosmosHub.GetBalance(ctx, cosmosAddr, firstHopDenomTrace.IBCDenom())
	require.NoError(t, err)
	require.True(t, cosmosUserBalance.Equal(math.NewInt(0)))

	// Parse the gas prices from the celestia config
	gasPrices, _ := sdk.ParseDecCoin(celestia.Config().GasPrices)

	// Fee is calculated as (gas spent * gas prices)
	fee := sdk.NewDecFromInt(math.NewInt(transferTx.GasSpent)).Mul(gasPrices.Amount)
	celBalanceAfter := initialBalance.Sub(transferAmount).Sub(fee.TruncateInt())
	require.Equal(t, response.Balances[0].Amount, celBalanceAfter.String())

	// Cosmos 2 user balance should be the transfer amount
	cosmosUser2Balance, err := cosmosHub2.GetBalance(ctx, cosmosAddr2, firstHopDenomTrace.IBCDenom())
	require.NoError(t, err)
	require.True(t, cosmosUser2Balance.Equal(transferAmount))
}
