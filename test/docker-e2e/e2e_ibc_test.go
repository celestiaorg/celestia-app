package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"fmt"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	"github.com/celestiaorg/tastora/framework/testutil/query"
	"github.com/celestiaorg/tastora/framework/testutil/sdkacc"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/docker/ibc"
	"github.com/celestiaorg/tastora/framework/docker/ibc/relayer"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	"golang.org/x/sync/errgroup"
)

// TestIBC tests IBC functionality by
// - deploying 2 chains
// - deploying hermes
// - creating an IBC connection and channel
// - performs a token transfer
// - upgrades from app version N-1 to N
// - performs token transfer over the same channel
// - creates a new connection and channel after upgrade
// - performs token transfer over a new channel
func (s *CelestiaTestSuite) TestIBC() {
	if testing.Short() {
		s.T().Skip("skipping IBC test in short mode")
	}

	ctx := context.Background()
	t := s.T()

	baseAppVersion := appconsts.Version - 1
	targetAppVersion := appconsts.Version
	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err, "failed to get celestia tag")

	var chainA, chainB tastoratypes.Chain
	var hermes *relayer.Hermes
	var channel ibc.Channel

	t.Cleanup(func() {
		if err := chainA.Stop(ctx); err != nil {
			t.Logf("Error stopping chain A: %v", err)
		}
		if err := chainB.Stop(ctx); err != nil {
			t.Logf("Error stopping chain B: %v", err)
		}
	})

	t.Cleanup(func() {
		if err := hermes.Stop(ctx); err != nil {
			t.Logf("Error stopping hermes: %v", err)
		}
	})

	t.Run("setup_chains", func(t *testing.T) {
		t.Logf("Creating celestia and simapp chains")
		chainA, chainB = s.setupIBCChains(ctx, tag, baseAppVersion)
	})

	t.Run("create_hermes", func(t *testing.T) {
		t.Logf("Creating hermes relayer")
		hermes = s.createHermesRelayer(ctx, chainA, chainB)
	})

	t.Run("create_clients", func(t *testing.T) {
		t.Logf("Creating IBC clients")
		err := hermes.CreateClients(ctx, chainA, chainB)
		s.Require().NoError(err, "failed to create IBC clients")
	})

	t.Run("setup_connection_and_channel", func(t *testing.T) {
		t.Logf("Creating IBC connection and channel")
		_, channel = s.establishIBCConnection(ctx, chainA, chainB, hermes)
	})

	t.Run("start_relayer", func(t *testing.T) {
		t.Logf("Starting hermes relayer")
		err := hermes.Start(ctx)
		s.Require().NoError(err, "failed to start hermes relayer")
	})

	t.Run("initial_transfers", func(t *testing.T) {
		t.Logf("Performing initial transfers")
		s.testTokenTransfers(ctx, chainA, chainB, channel)
	})

	t.Run("upgrade_app_version", func(t *testing.T) {
		t.Logf("Upgrading app version from %d to %d", baseAppVersion, targetAppVersion)
		s.upgradeChain(ctx, chainA, targetAppVersion)
	})

	t.Run("existing_channel_transfers", func(t *testing.T) {
		s.testTokenTransfers(ctx, chainA, chainB, channel)
	})

	t.Run("new_connection_and_channel", func(t *testing.T) {
		t.Logf("Creating new connection and channel after upgrade")
		_, channel = s.establishIBCConnection(ctx, chainA, chainB, hermes)
	})

	t.Run("new_channel_transfers", func(t *testing.T) {
		t.Logf("Performing transfers over new channel")
		s.testTokenTransfers(ctx, chainA, chainB, channel)
	})
}

// setupIBCChains creates and starts two chains: Celestia app (chain A) and IBC-Go simapp (chain B)
func (s *CelestiaTestSuite) setupIBCChains(ctx context.Context, imageTag string, appVersion uint64) (tastoratypes.Chain, tastoratypes.Chain) {
	t := s.T()

	s.celestiaCfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(imageTag)
	s.celestiaCfg.Genesis = s.celestiaCfg.Genesis.WithAppVersion(appVersion)

	var chainA, chainB tastoratypes.Chain
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		chainA, err = dockerchain.NewCelestiaChainBuilder(t, s.celestiaCfg).Build(gCtx)
		if err != nil {
			return fmt.Errorf("failed to build chain A: %w", err)
		}
		return chainA.Start(gCtx)
	})

	g.Go(func() error {
		var err error
		builder := newSimappChainBuilder(t, s.celestiaCfg)
		chainB, err = builder.Build(gCtx)
		if err != nil {
			return fmt.Errorf("failed to build chain B: %w", err)
		}
		return chainB.Start(gCtx)
	})

	s.Require().NoError(g.Wait(), "failed to setup chains")

	return chainA, chainB
}

// createHermesRelayer creates and initializes Hermes relayer without starting it
func (s *CelestiaTestSuite) createHermesRelayer(ctx context.Context, chainA, chainB tastoratypes.Chain) *relayer.Hermes {
	t := s.T()

	hermes, err := relayer.NewHermes(ctx, s.client, t.Name(), s.network, 0, s.logger)
	s.Require().NoError(err, "failed to create hermes")

	err = hermes.Init(ctx, chainA, chainB)
	s.Require().NoError(err, "failed to initialize hermes")

	return hermes
}

// establishIBCConnection creates IBC clients, connection, and channel between the chains
func (s *CelestiaTestSuite) establishIBCConnection(ctx context.Context, chainA, chainB tastoratypes.Chain, hermes *relayer.Hermes) (ibc.Connection, ibc.Channel) {
	connection, err := hermes.CreateConnections(ctx, chainA, chainB)
	s.Require().NoError(err, "failed to create IBC connection")

	channelOpts := ibc.CreateChannelOptions{
		SourcePortName: "transfer",
		DestPortName:   "transfer",
		Order:          ibc.OrderUnordered,
		Version:        "ics20-1",
	}

	channel, err := hermes.CreateChannel(ctx, chainA, connection, channelOpts)
	s.Require().NoError(err, "failed to create IBC channel")

	return connection, channel
}

// testTokenTransfers tests token transfers from Celestia to simapp
func (s *CelestiaTestSuite) testTokenTransfers(ctx context.Context, chainA, chainB tastoratypes.Chain, channel ibc.Channel) {
	s.transferTokens(ctx, chainA, chainB, channel, "utia", 100000)
}

// transferTokens performs an IBC token transfer from Celestia to simapp
func (s *CelestiaTestSuite) transferTokens(ctx context.Context, sourceChain, destChain tastoratypes.Chain, channel ibc.Channel, denom string, amount int64) {
	sourceWallet := sourceChain.GetFaucetWallet()
	destWallet := destChain.GetFaucetWallet()
	ibcTransfer := s.createIBCTransferMsg(sourceWallet, destWallet, channel, denom, amount)

	// Submit transaction and verify results - always use TxClient since we only transfer from Celestia
	s.submitTransactionAndVerify(ctx, sourceChain, destChain, ibcTransfer, denom, amount)
}

// createIBCTransferMsg creates an IBC transfer message
func (s *CelestiaTestSuite) createIBCTransferMsg(sourceWallet, destWallet tastoratypes.Wallet, channel ibc.Channel, denom string, amount int64) *ibctransfertypes.MsgTransfer {
	destAddr, err := sdkacc.AddressFromWallet(destWallet)
	s.Require().NoError(err, "failed to parse destination address")

	transferAmount := sdkmath.NewInt(amount)
	return ibctransfertypes.NewMsgTransfer(
		channel.PortID,
		channel.ChannelID,
		sdk.NewCoin(denom, transferAmount),
		sourceWallet.GetFormattedAddress(),
		destAddr.String(),
		clienttypes.ZeroHeight(),
		uint64(time.Now().Add(time.Hour).UnixNano()),
		"",
	)
}

// submitTransactionAndVerify submits a transaction using the appropriate method and verifies results
func (s *CelestiaTestSuite) submitTransactionAndVerify(ctx context.Context, sourceChain, destChain tastoratypes.Chain, msg *ibctransfertypes.MsgTransfer, denom string, amount int64) {
	channel := ibc.Channel{PortID: msg.SourcePort, CounterpartyID: msg.SourceChannel}
	ibcDenom := s.calculateIBCDenom(channel, denom)

	destWallet := destChain.GetFaucetWallet()
	destBalance := s.getBalance(ctx, destChain, destWallet.GetFormattedAddress(), ibcDenom)

	txClient, err := s.setupTxClient(ctx, sourceChain)
	s.Require().NoError(err, "failed to setup tx client for celestia chain")

	resp, err := txClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(200000), user.SetFee(5000))
	s.Require().NoError(err, "failed to submit IBC transfer via TxClient")

	s.Require().Equal(uint32(0), resp.Code, "IBC transfer tx failed with code %d", resp.Code)

	err = wait.ForBlocks(ctx, 5, sourceChain)
	s.Require().NoError(err, "failed to wait for blocks")

	transferAmount := sdkmath.NewInt(amount)
	s.verifyTransferResults(ctx, destChain, destWallet, ibcDenom, destBalance, transferAmount)
}

// verifyTransferResults checks the final balances and verifies the transfer results
func (s *CelestiaTestSuite) verifyTransferResults(ctx context.Context, destChain tastoratypes.Chain, destWallet tastoratypes.Wallet, ibcDenom string, destBalance, transferAmount sdkmath.Int) {
	finalDestBalance := s.getBalance(ctx, destChain, destWallet.GetFormattedAddress(), ibcDenom)

	expectedDestBalance := destBalance.Add(transferAmount)
	s.Require().True(finalDestBalance.Equal(expectedDestBalance),
		"destination balance mismatch: expected %s, got %s", expectedDestBalance.String(), finalDestBalance.String())
}

// upgradeChain upgrades the celestia chain from baseAppVersion to targetAppVersion
// This reuses the existing upgrade logic from e2e_upgrade_test.go
func (s *CelestiaTestSuite) upgradeChain(ctx context.Context, chain tastoratypes.Chain, targetAppVersion uint64) {
	validatorNode := chain.GetNodes()[0]
	cfg := s.celestiaCfg
	kr := cfg.Genesis.Keyring()

	records, err := kr.List()
	s.Require().NoError(err, "failed to list keyring records")

	upgradeHeight := s.signalAndGetUpgradeHeight(ctx, chain, validatorNode, cfg, records, targetAppVersion)

	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	status, err := rpcClient.Status(ctx)
	s.Require().NoError(err, "failed to get node status")
	currentHeight := status.SyncInfo.LatestBlockHeight

	blocksToWait := int(upgradeHeight-currentHeight) + 2
	s.Require().NoError(wait.ForBlocks(ctx, blocksToWait, chain))

	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(targetAppVersion, abciInfo.Response.GetAppVersion(), "app version mismatch after upgrade")
}

// setupTxClient sets up a tx client using the node's keyring
func (s *CelestiaTestSuite) setupTxClient(ctx context.Context, chain tastoratypes.Chain) (*user.TxClient, error) {
	node := chain.GetNodes()[0].(*docker.ChainNode)

	keyring, err := node.GetKeyring()
	if err != nil {
		return nil, err
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	return user.SetupTxClient(
		ctx,
		keyring,
		node.GrpcConn,
		encCfg,
	)
}

// getBalance gets the balance of a specific denom for an address
func (s *CelestiaTestSuite) getBalance(ctx context.Context, chain tastoratypes.Chain, address, denom string) sdkmath.Int {
	dockerChain, ok := chain.(*docker.Chain)
	if !ok {
		s.T().Logf("Chain is not a docker Chain, returning zero balance")
		return sdkmath.ZeroInt()
	}

	node := dockerChain.GetNode()
	if node.GrpcConn == nil {
		s.T().Logf("GRPC connection is nil for chain %s, returning zero balance", chain.GetChainID())
		return sdkmath.ZeroInt()
	}

	amount, err := query.Balance(ctx, node.GrpcConn, address, denom)
	s.Require().NoError(err, "failed to query balance for %s %s", address, denom)
	return amount
}

// calculateIBCDenom calculates the IBC denomination using ibc-go utilities
func (s *CelestiaTestSuite) calculateIBCDenom(channel ibc.Channel, baseDenom string) string {
	prefixedDenom := ibctransfertypes.GetPrefixedDenom(
		channel.PortID,
		channel.CounterpartyID,
		baseDenom,
	)
	return ibctransfertypes.ParseDenomTrace(prefixedDenom).IBCDenom()
}

// newSimappChainBuilder builds a standard simapp chain without token filters
func newSimappChainBuilder(t *testing.T, cfg *dockerchain.Config) *tastoradockertypes.ChainBuilder {
	encodingConfig := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)
	return tastoradockertypes.NewChainBuilder(t).
		WithEncodingConfig(&encodingConfig).
		WithName("simapp").
		WithChainID("chain-b").
		// TODO: this is a custom built simapp that has the bech32prefix as "celestia" as a workaround for the global
		// SDK config not being usable when 2 chains have a different beck32 preix (e.g. "celestia" and "cosmos" ) because it is sealed.
		WithImage(tastoracontainertypes.NewImage("ghcr.io/chatton/ibc-go-simd", "v8.5.0", "1000:1000")).
		WithBinaryName("simd").
		WithBech32Prefix("celestia").
		WithDenom("utia").
		WithGasPrices("0.000001utia").
		WithDockerNetworkID(cfg.DockerNetworkID).
		WithDockerClient(cfg.DockerClient).
		WithChainID("chain-b").
		WithNode(tastoradockertypes.NewChainNodeConfigBuilder().Build())
}
