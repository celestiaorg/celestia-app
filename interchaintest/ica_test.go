package interchaintest_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"
	"github.com/strangelove-ventures/interchaintest/v6/relayer"
	"github.com/strangelove-ventures/interchaintest/v6/testreporter"
	"github.com/strangelove-ventures/interchaintest/v6/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	relayerName     = "relayerName"
	path            = "path"
	DefaultGasValue = 500_000_0000
)

// TestICA verifies that Interchain Accounts work as expected.
func TestICA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestICA in short mode.")
	}

	client, network := interchaintest.DockerSetup(t)
	celestia, stride := getChains(t)

	relayer := interchaintest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t),
		relayer.RelayerOptionExtraStartFlags{Flags: []string{"-p", "events", "-b", "100"}},
	).Build(t, client, network)
	ic := interchaintest.NewInterchain().
		AddChain(celestia).
		AddChain(stride).
		AddRelayer(relayer, relayerName).
		AddLink(interchaintest.InterchainLink{
			Chain1:  celestia,
			Chain2:  stride,
			Relayer: relayer,
			Path:    path,
		})

	ctx := context.Background()
	reporter := testreporter.NewNopReporter().RelayerExecReporter(t)
	err := ic.Build(ctx, reporter, interchaintest.InterchainBuildOptions{
		TestName:  t.Name(),
		Client:    client,
		NetworkID: network,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ic.Close() })

	// err = relayer.CreateClients(ctx, reporter, path, ibc.CreateClientOptions{TrustingPeriod: "330h"})
	// require.NoError(t, err)

	// err = testutil.WaitForBlocks(ctx, 2, celestia, stride)
	// require.NoError(t, err)

	// err = relayer.CreateConnections(ctx, reporter, path)
	// require.NoError(t, err)

	// err = relayer.StartRelayer(ctx, reporter, path)
	// require.NoError(t, err)

	// err = testutil.WaitForBlocks(ctx, 2, celestia, stride)
	// require.NoError(t, err)

	// connections, err := relayer.GetConnections(ctx, reporter, celestia.Config().ChainID)
	// require.NoError(t, err)
	// require.Len(t, connections, 1)

	// connections, err = relayer.GetConnections(ctx, reporter, stride.Config().ChainID)
	// require.NoError(t, err)
	// require.Len(t, connections, 1)

	// amount := math.NewIntFromUint64(uint64(10_000_000_000))
	// users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), amount, celestia, stride)

	// celestiaUser, strideUser := users[0], users[1]
	// celestiaAddr := celestiaUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(celestia.Config().Bech32Prefix)
	// strideAddr := strideUser.(*cosmos.CosmosWallet).FormattedAddressWithPrefix(stride.Config().Bech32Prefix)
	// fmt.Printf("celestiaAddr: %s, strideAddr: %v\n", celestiaAddr, strideAddr)

	// version := icatypes.NewDefaultMetadataString(ibctesting.FirstConnectionID, ibctesting.FirstConnectionID)
	// msgRegisterInterchainAccount := controllertypes.NewMsgRegisterInterchainAccount(ibctesting.FirstConnectionID, strideAddr, version)
	// txResp := BroadcastMessages(t, ctx, celestia, stride, strideUser, msgRegisterInterchainAccount)
	// fmt.Printf("txResp %v\n", txResp)

	// celestiaHeight, err := celestia.Height(ctx)
	// require.NoError(t, err)

	// isChannelOpen := func(found *chantypes.MsgChannelOpenConfirm) bool {
	// 	return found.PortId == "icahost"
	// }
	// _, err = cosmos.PollForMessage(ctx, celestia, cosmos.DefaultEncoding().InterfaceRegistry, celestiaHeight, celestiaHeight+30, isChannelOpen)
	// require.NoError(t, err)

	// // Query for the newly registered interchain account
	// queryICA := []string{
	// 	gaia.Config().Bin, "query", "interchain-accounts", "controller", "interchain-account", gaiaAddr, connections[0].ID,
	// 	"--chain-id", gaia.Config().ChainID,
	// 	"--home", gaia.HomeDir(),
	// 	"--node", gaia.GetRPCAddress(),
	// }
	// stdout, _, err := gaia.Exec(ctx, queryICA, nil)
	// require.NoError(t, err)
	// t.Log(stdout)
}

func BroadcastMessages(t *testing.T, ctx context.Context, celestia ibc.Chain, gaia ibc.Chain, user ibc.Wallet, msgs ...sdk.Msg) sdk.TxResponse {
	cosmosChain, ok := gaia.(*cosmos.CosmosChain)
	require.True(t, ok, "BroadcastMessages expects a cosmos.CosmosChain")

	broadcaster := cosmos.NewBroadcaster(t, cosmosChain)
	// broadcaster.ConfigureFactoryOptions(func(factory tx.Factory) tx.Factory {
	// 	return factory.WithGas(DefaultGasValue)
	// })

	broadcaster.ConfigureClientContextOptions(func(clientContext client.Context) client.Context {
		// use a codec with all the types our tests care about registered.
		// BroadcastTx will deserialize the response and will not be able to otherwise.
		cdc := cosmosChain.Config().EncodingConfig.Codec
		fmt.Printf("cdc %#v\n", cdc)
		// cosmosChain.Config().EncodingConfig.TxConfig
		return clientContext.WithCodec(cdc)
	})
	// broadcaster.ConfigureClientContextOptions(func(clientContext client.Context) client.Context {
	// 	// use a codec with all the types our tests care about registered.
	// 	// BroadcastTx will deserialize the response and will not be able to otherwise.
	// 	cdc := Codec()
	// 	return clientContext.WithCodec(cdc).WithTxConfig(authtx.NewTxConfig(cdc, []signingtypes.SignMode{signingtypes.SignMode_SIGN_MODE_DIRECT}))
	// })

	txResp, err := cosmos.BroadcastTx(ctx, broadcaster, user, msgs...)
	require.NoError(t, err)
	fmt.Printf("txResp %v\n", txResp)
	require.Equal(t, uint32(0), txResp.Code)
	require.NotEmpty(t, txResp.TxHash)
	require.NotEqual(t, int64(0), txResp.GasUsed)
	require.NotEqual(t, int64(0), txResp.GasWanted)
	require.NotEmpty(t, txResp.Events)
	require.NotEmpty(t, txResp.Data)

	err = testutil.WaitForBlocks(ctx, 2, celestia, gaia)
	require.NoError(t, err)
	return txResp
}
