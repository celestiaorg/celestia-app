package interchain

import (
	"context"
	"fmt"
	// "strings"
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/test/interchain/chainspec"
	// bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	// "github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v6/testreporter"
	"github.com/strangelove-ventures/interchaintest/v6/testutil"
	// "github.com/stretchr/testify/assert"
	transfertypes "github.com/cosmos/ibc-go/v6/modules/apps/transfer/types"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"
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
	// t.Skip("skipping TestPacketForwardMiddleware in short mode.")
	// }

	client, network := interchaintest.DockerSetup(t)
	celestia := chainspec.GetCelestia(t)
	cosmosHub := chainspec.GetCosmosHub(t)
	cosmosHub2 := chainspec.GetCosmosHub2(t)
	relayer := getRelayerFactory(t).Build(t, client, network)

	// NINA: SETTING UP PATHS BETWEEN THE 3 CHAINS
	pathCosmosCelestia := fmt.Sprintf("%s%s", cosmosHub.Config().ChainID, celestia.Config().ChainID)
	pathCelestiaCosmos2 := fmt.Sprintf("%s%s", celestia.Config().ChainID, cosmosHub2.Config().ChainID)

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
	require.Len(t, celestiaConnections, 2)

	cosmosConnections, err := relayer.GetConnections(ctx, reporter, cosmosHub.Config().ChainID)
	require.NoError(t, err)
	fmt.Println(cosmosConnections, "COSMOS CONNECTIONS")
	require.Len(t, cosmosConnections, 2) // 2 connections: the first is connection-0 and the second is connection-localhost.

	// NINA: GETTING USERS AND THEIR ADDRESSES TO USE IN THE TEST (done for all 3 chains!!)
	initialBalance := math.NewInt(10_000_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), initialBalance, celestia, cosmosHub, cosmosHub2)
	err = testutil.WaitForBlocks(ctx, 2, celestia, cosmosHub, cosmosHub2)
	require.NoError(t, err)
	fmt.Println(users, "USERS")

	// NINA: GETTING USERS AND THEIR ADDRESSES TO USE IN THE TEST (done for all 3 chains!!)
	celestiaUser, cosmosUser, cosmosUser2 := users[0], users[1], users[2]
	celestiaAddr := celestiaUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(celestia.Config().Bech32Prefix)
	cosmosAddr := cosmosUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(cosmosHub.Config().Bech32Prefix)
	cosmosAddr2 := cosmosUser2.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(cosmosHub2.Config().Bech32Prefix)
	fmt.Printf("celestiaAddr: %s, cosmosAddr: %v, cosmosAddr2: %v\n", celestiaAddr, cosmosAddr, cosmosAddr2)

	// create channels
	cosmosCelestiaChan, err := ibc.GetTransferChannel(ctx, relayer, reporter, cosmosHub.Config().ChainID, celestia.Config().ChainID)
	fmt.Println(cosmosCelestiaChan, "COSMOS CELESTIA CHANNEL")
	require.NoError(t, err)
	celestiaCosmos2Chan, err := ibc.GetTransferChannel(ctx, relayer, reporter, celestia.Config().ChainID, cosmosHub2.Config().ChainID)
	require.NoError(t, err)

	transferAmount := math.NewInt(100_000)
	// zeroBalance := math.NewInt(0)

	firstHopDenom := transfertypes.GetPrefixedDenom(cosmosCelestiaChan.PortID, cosmosCelestiaChan.ChannelID, celestia.Config().Denom)
	fmt.Println(firstHopDenom, "FIRST HOP DENOM")
	secondHopDenom := transfertypes.GetPrefixedDenom(celestiaCosmos2Chan.PortID, celestiaCosmos2Chan.ChannelID, celestia.Config().Denom)
	fmt.Println(secondHopDenom, "SECOND HOP DENOM")

	firstHopDenomTrace := transfertypes.ParseDenomTrace(firstHopDenom)
	fmt.Println(firstHopDenomTrace, "FIRST HOP DENOM TRACE")
	secondHopDenomTrace := transfertypes.ParseDenomTrace(secondHopDenom)
	fmt.Println(secondHopDenomTrace, "SECOND HOP DENOM TRACE")

	fmt.Println(firstHopDenomTrace.IBCDenom(), "FIRST HOP DENOM IBC DENOM")
	fmt.Println(secondHopDenomTrace.IBCDenom(), "SECOND HOP DENOM IBC DENOM")
	// path
	// fmt.Println(firstHopDenomTrace.Path(), "FIRST HOP DENOM PATH")
	// fmt.Println(secondHopDenomTrace.Path(), "SECOND HOP DENOM PATH")

	// Send packet from CosmosHub->Celestia->CosmosHub2
	transfer := ibc.WalletAmount{
		Address: cosmosAddr,
		Denom:   celestia.Config().Denom,
		Amount:  transferAmount,
	}
	fmt.Println(transfer, "TRANSFER")

	fmt.Println(secondHopDenom, "FIRST HOP DENOM")

	secondHopMetadata := &PacketMetadata{
		Forward: &ForwardMetadata{
			Receiver: cosmosAddr2,
			Channel:  celestiaCosmos2Chan.ChannelID,
			Port:     celestiaCosmos2Chan.PortID,
		},
	}

	nextBz, err := json.Marshal(secondHopMetadata)
	require.NoError(t, err)
	next := string(nextBz)
	// firs hop send some tia to cosmos chain
	firstHopMetadata := &PacketMetadata{
		Forward: &ForwardMetadata{
			Receiver: cosmosAddr,
			Channel:  cosmosCelestiaChan.ChannelID,
			Port:     cosmosCelestiaChan.PortID,
			Next:     &next,
		},
	}

	memo, err := json.Marshal(firstHopMetadata)
	require.NoError(t, err)

	celHeight, err := celestia.Height(ctx)
	require.NoError(t, err)
	fmt.Println(celHeight, "COSMOS HUB HEIGHT")

	transferTx, err := celestia.SendIBCTransfer(ctx, cosmosCelestiaChan.ChannelID, celestiaUser.KeyName(), transfer, ibc.TransferOptions{Memo: string(memo)})
	require.NoError(t, err)

	_, err = testutil.PollForAck(ctx, celestia, celHeight, celHeight+30, transferTx.Packet)
	require.NoError(t, err)
	err = testutil.WaitForBlocks(ctx, 3, celestia)
	require.NoError(t, err)

	queryBalance := []string{
		celestia.Config().Bin, "query", "bank", "balances", celestiaAddr,
		"--chain-id", celestia.Config().ChainID,
		"--home", celestia.HomeDir(),
		"--node", celestia.GetRPCAddress(),
	}
	stdout, stderr, err := celestia.Exec(ctx, queryBalance, nil)
	require.NoError(t, err)
	require.Empty(t, stderr)
	fmt.Println(string(stdout), "STDOUT")
	t.Logf("stdout %v\n", string(stdout))
	// assert.NotEmpty(t, string(stdout))

	queryBalanceHub2 := []string{
		cosmosHub2.Config().Bin, "query", "bank", "balances", cosmosAddr2,
		"--chain-id", cosmosHub2.Config().ChainID,
		"--home", cosmosHub2.HomeDir(),
		"--node", cosmosHub2.GetRPCAddress(),
	}
	stdoutt, stderrr, err := cosmosHub2.Exec(ctx, queryBalanceHub2, nil)
	require.NoError(t, err)
	require.Empty(t, stderrr)
	fmt.Println(string(stdoutt), "STDOUT")
	t.Logf("stdout %v\n", string(stdoutt))

	queryBalanceHub := []string{
		cosmosHub.Config().Bin, "query", "bank", "balances", cosmosAddr,
		"--chain-id", cosmosHub.Config().ChainID,
		"--home", cosmosHub.HomeDir(),
		"--node", cosmosHub.GetRPCAddress(),
	}
	stdouttt, stderrrr, err := cosmosHub.Exec(ctx, queryBalanceHub, nil)
	require.NoError(t, err)
	require.Empty(t, stderrrr)
	fmt.Println(string(stdouttt), "STDOUT")
	t.Logf("stdout %v\n", string(stdoutt))

	// require.NoError(t, err)
	// fmt.Println(comos2Balance, "COSMOS 2 BALANCE")

	// cosmosBalance, err := cosmosHub.GetBalance(ctx, cosmosAddr, firstHopDenomTrace.IBCDenom())
	// require.NoError(t, err)
	// // require.True(t, cosmosBalance.Equal(math.NewInt(0)))
	// fmt.Println(cosmosBalance, "COSMOS BALANCE")

	cosmosBalance2, err := cosmosHub2.GetBalance(ctx, cosmosAddr2, firstHopDenomTrace.IBCDenom())
	require.NoError(t, err)
	// require.True(t, cosmosBalance2.Equal(transferAmount))
	fmt.Println(cosmosBalance2, "COSMOS 2 BALANCE")
	// require.True()

	// require.True(t, cosmosBalance.Equal(initialBalance.Sub(transferAmount)))
	// fmt.Println(initialBalance.Sub(transferAmount).Sub(math.NewInt(int64(feeRateDecimal))), "INITIAL BALANCE with fee")

	// require.True(t, celestiaBalance.Equal(math.NewInt(0)))
	// require.True(t, comos2Balance.Equal(math.NewInt(0)))

	// Send packet from CosmosHub->Celestia->CosmosHub2
	// Send packet from CosmosHub->Celestia->CosmosHub2
	transferTest := ibc.WalletAmount{
		Address: celestiaAddr,
		Denom:   cosmosHub.Config().Denom,
		Amount:  transferAmount,
	}

	firstHopMetadataTest := &PacketMetadata{
		Forward: &ForwardMetadata{
			Receiver: cosmosAddr2,
			Channel:  celestiaCosmos2Chan.ChannelID,
			Port:     celestiaCosmos2Chan.PortID,
			Next:     nil,
		},
	}

	memoTest, err := json.Marshal(firstHopMetadataTest)
	fmt.Println(memoTest, "MEMO")
	require.NoError(t, err)

	celHeightTest, err := cosmosHub.Height(ctx)
	require.NoError(t, err)
	fmt.Println(celHeightTest, "COSMOS HUB HEIGHT")

	transferTxTest, err := cosmosHub.SendIBCTransfer(ctx, cosmosCelestiaChan.ChannelID, cosmosUser.KeyName(), transferTest, ibc.TransferOptions{Memo: string(memoTest)})
	require.NoError(t, err)
	fmt.Println(transferTxTest, "TRANSFER TX")
	// _, err = testutil.PollForAck(ctx, cosmosHub, celHeightTest, celHeightTest+30, transferTxTest.Packet)
	// require.NoError(t, err)
	// err = testutil.WaitForBlocks(ctx, 3, cosmosHub)
	require.NoError(t, err)
}
