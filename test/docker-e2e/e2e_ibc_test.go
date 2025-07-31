package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v5/pkg/user"
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

// TestTastoraIBC tests IBC functionality with token filtering and upgrades.
// This test follows the requirements from https://github.com/celestiaorg/celestia-app/issues/5387
func (s *CelestiaTestSuite) TestTastoraIBC() {
	if testing.Short() {
		s.T().Skip("skipping tastora IBC test in short mode")
	}

	ctx := context.Background()
	t := s.T()

	// Start with version 4, upgrade to version 5
	baseAppVersion := uint64(4)
	targetAppVersion := uint64(5)

	// Get celestia image tag
	//tag, err := dockerchain.GetCelestiaTagStrict()
	//s.Require().NoError(err)

	tag := dockerchain.GetCelestiaTag()

	// Setup chains: Celestia app (version N) and IBC-Go simapp
	chainA, chainB := s.setupIBCChains(ctx, tag, baseAppVersion)

	// Cleanup
	t.Cleanup(func() {
		if err := chainA.Stop(ctx); err != nil {
			t.Logf("Error stopping chain A: %v", err)
		}
		if err := chainB.Stop(ctx); err != nil {
			t.Logf("Error stopping chain B: %v", err)
		}
	})

	// Create and initialize Hermes relayer (but don't start it yet)
	hermes := s.createHermesRelayer(ctx, chainA, chainB)
	t.Cleanup(func() {
		if hermes != nil {
			if err := hermes.Stop(ctx); err != nil {
				t.Logf("Error stopping hermes: %v", err)
			}
		}
	})

	// Establish IBC connection and channel before starting relayer
	connection, channel := s.establishIBCConnection(ctx, chainA, chainB, hermes)

	// Now start the relayer for packet processing
	err := hermes.Start(ctx)
	s.Require().NoError(err, "failed to start hermes relayer")

	// Test bidirectional token transfers with token filter verification
	s.testTokenTransfers(ctx, chainA, chainB, channel)

	// Upgrade chain A to version N+1
	s.upgradeChain(ctx, chainA, baseAppVersion, targetAppVersion)

	// Continue token transfers on existing channel after upgrade
	s.testTokenTransfersAfterUpgrade(ctx, chainA, chainB, channel, "existing channel")

	// Create new channel after upgrade and perform token transfers
	newChannel := s.createNewChannelAfterUpgrade(ctx, chainA, chainB, hermes, connection)
	s.testTokenTransfersAfterUpgrade(ctx, chainA, chainB, newChannel, "new channel")
}

// setupIBCChains creates and starts two chains: Celestia app (chain A) and IBC-Go simapp (chain B)
func (s *CelestiaTestSuite) setupIBCChains(ctx context.Context, imageTag string, appVersion uint64) (tastoratypes.Chain, tastoratypes.Chain) {
	t := s.T()

	// Chain A configuration (Celestia app)
	cfgA := dockerchain.DefaultConfig(s.client, s.network).WithTag(imageTag)
	cfgA.Genesis = cfgA.Genesis.WithAppVersion(appVersion)

	// Create chains in parallel
	var chainA, chainB tastoratypes.Chain
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		chainA, err = dockerchain.NewCelestiaChainBuilder(t, cfgA).Build(gCtx)
		if err != nil {
			return fmt.Errorf("failed to build chain A: %w", err)
		}
		return chainA.Start(gCtx)
	})

	g.Go(func() error {
		var err error
		builder := dockerchain.NewSimappChainBuilder(t, cfgA)
		chainB, err = builder.Build(gCtx)
		if err != nil {
			return fmt.Errorf("failed to build chain B: %w", err)
		}
		return chainB.Start(gCtx)
	})

	s.Require().NoError(g.Wait(), "failed to setup chains")

	t.Log("Successfully setup IBC chains")
	return chainA, chainB
}

// createHermesRelayer creates and initializes Hermes relayer without starting it
func (s *CelestiaTestSuite) createHermesRelayer(ctx context.Context, chainA, chainB tastoratypes.Chain) *relayer.Hermes {
	t := s.T()

	// Create and initialize Hermes relayer
	hermes, err := relayer.NewHermes(ctx, s.client, t.Name(), s.network, 0, s.logger)
	s.Require().NoError(err, "failed to create hermes")

	err = hermes.Init(ctx, chainA, chainB)
	s.Require().NoError(err, "failed to initialize hermes")

	t.Log("Successfully created and initialized Hermes relayer")
	return hermes
}

// establishIBCConnection creates IBC clients, connection, and channel between the chains
func (s *CelestiaTestSuite) establishIBCConnection(ctx context.Context, chainA, chainB tastoratypes.Chain, hermes *relayer.Hermes) (ibc.Connection, ibc.Channel) {
	t := s.T()

	// Create IBC clients
	err := hermes.CreateClients(ctx, chainA, chainB)
	s.Require().NoError(err, "failed to create IBC clients")

	// Create connection
	connection, err := hermes.CreateConnections(ctx, chainA, chainB)
	s.Require().NoError(err, "failed to create IBC connection")

	// Create channel for token transfers
	channelOpts := ibc.CreateChannelOptions{
		SourcePortName: "transfer",
		DestPortName:   "transfer",
		Order:          ibc.OrderUnordered,
		Version:        "ics20-1",
	}

	channel, err := hermes.CreateChannel(ctx, chainA, connection, channelOpts)
	s.Require().NoError(err, "failed to create IBC channel")

	t.Logf("Successfully established IBC connection and channel: %s", channel.ChannelID)
	return connection, channel
}

// testTokenTransfers tests bidirectional token transfers with token filter verification
func (s *CelestiaTestSuite) testTokenTransfers(ctx context.Context, chainA, chainB tastoratypes.Chain, channel ibc.Channel) {
	t := s.T()

	// Test 1: Send tokens from Chain A (Celestia) to Chain B (simapp) - should succeed
	t.Log("Testing token transfer from Celestia to simapp (should succeed)")
	s.transferTokens(ctx, chainA, chainB, channel, "utia", 100000, true)

	// Test 2: Send tokens from Chain B (simapp) to Chain A (Celestia) - should fail due to token filtering
	t.Log("Testing token transfer from simapp to Celestia (should fail due to token filtering)")
	s.transferTokens(ctx, chainB, chainA, channel, "stake", 100000, false)
}

// testTokenTransfersAfterUpgrade tests that token transfers work correctly after upgrade
func (s *CelestiaTestSuite) testTokenTransfersAfterUpgrade(ctx context.Context, chainA, chainB tastoratypes.Chain, channel ibc.Channel, channelType string) {
	t := s.T()

	t.Logf("Testing token transfers after upgrade on %s", channelType)

	// Test transfers still work after upgrade - A to B should succeed
	t.Logf("Testing token transfer from Celestia to simapp on %s after upgrade (should succeed)", channelType)
	s.transferTokens(ctx, chainA, chainB, channel, "utia", 100000, true)

	// Token filtering should still work - B to A should fail
	t.Logf("Testing token transfer from simapp to Celestia on %s after upgrade (should fail)", channelType)
	s.transferTokens(ctx, chainB, chainA, channel, "stake", 100000, false)
}

// transferTokens performs an IBC token transfer and verifies the result
func (s *CelestiaTestSuite) transferTokens(ctx context.Context, sourceChain, destChain tastoratypes.Chain, channel ibc.Channel, denom string, amount int64, shouldSucceed bool) {
	t := s.T()

	// Get wallets
	sourceWallet := sourceChain.GetFaucetWallet()
	destWallet := destChain.GetFaucetWallet()

	destAddr, err := sdk.AccAddressFromBech32(destWallet.GetFormattedAddress())
	s.Require().NoError(err, "failed to parse destination address")

	// Create IBC transfer message
	transferAmount := sdkmath.NewInt(amount)
	ibcTransfer := ibctransfertypes.NewMsgTransfer(
		channel.PortID,
		channel.ChannelID,
		sdk.NewCoin(denom, transferAmount),
		sourceWallet.GetFormattedAddress(),
		destAddr.String(),
		clienttypes.ZeroHeight(),
		uint64(time.Now().Add(time.Hour).UnixNano()),
		"",
	)

	// Setup tx client for source chain
	var txClient *user.TxClient
	if sourceChain.GetChainID() == "chain-a" {
		// Celestia chain
		node := sourceChain.GetNodes()[0].(*docker.ChainNode)
		cfg := dockerchain.DefaultConfig(s.client, s.network)
		txClient, err = dockerchain.SetupTxClient(ctx, node, cfg)
		s.Require().NoError(err, "failed to setup tx client for celestia chain")
	} else {
		// Simapp chain - setup with basic config
		txClient, err = s.setupSimappTxClient(ctx, sourceChain)
		s.Require().NoError(err, "failed to setup tx client for simapp chain")
	}

	// Get initial balances
	sourceBalance := s.getBalance(ctx, sourceChain, sourceWallet.GetFormattedAddress(), denom)
	ibcDenom := s.calculateIBCDenom(channel, denom)
	destBalance := s.getBalance(ctx, destChain, destWallet.GetFormattedAddress(), ibcDenom)

	t.Logf("Initial balances - Source: %s %s, Dest: %s %s", sourceBalance.String(), denom, destBalance.String(), ibcDenom)

	// Submit transfer
	resp, err := txClient.SubmitTx(ctx, []sdk.Msg{ibcTransfer}, user.SetGasLimit(200000), user.SetFee(5000))
	s.Require().NoError(err, "failed to submit IBC transfer")
	s.Require().Equal(uint32(0), resp.Code, "IBC transfer tx failed with code %d", resp.Code)

	// Wait for packet processing
	time.Sleep(10 * time.Second)
	err = wait.ForBlocks(ctx, 3, sourceChain)
	s.Require().NoError(err, "failed to wait for blocks")

	// Check final balances
	finalSourceBalance := s.getBalance(ctx, sourceChain, sourceWallet.GetFormattedAddress(), denom)
	finalDestBalance := s.getBalance(ctx, destChain, destWallet.GetFormattedAddress(), ibcDenom)

	t.Logf("Final balances - Source: %s %s, Dest: %s %s", finalSourceBalance.String(), denom, finalDestBalance.String(), ibcDenom)

	if shouldSucceed {
		// Verify tokens were transferred
		expectedSourceBalance := sourceBalance.Sub(transferAmount)
		expectedDestBalance := destBalance.Add(transferAmount)

		s.Require().True(finalSourceBalance.Equal(expectedSourceBalance),
			"source balance mismatch: expected %s, got %s", expectedSourceBalance.String(), finalSourceBalance.String())
		s.Require().True(finalDestBalance.Equal(expectedDestBalance),
			"destination balance mismatch: expected %s, got %s", expectedDestBalance.String(), finalDestBalance.String())

		t.Log("Token transfer succeeded as expected")
	} else {
		// Verify tokens were NOT transferred (due to token filtering)
		s.Require().True(finalSourceBalance.Equal(sourceBalance),
			"source balance should remain unchanged, expected %s, got %s", sourceBalance.String(), finalSourceBalance.String())
		s.Require().True(finalDestBalance.Equal(destBalance),
			"destination balance should remain unchanged, expected %s, got %s", destBalance.String(), finalDestBalance.String())

		t.Log("Token transfer failed as expected due to token filtering")
	}
}

// upgradeChain upgrades the celestia chain from baseAppVersion to targetAppVersion
func (s *CelestiaTestSuite) upgradeChain(ctx context.Context, chain tastoratypes.Chain, baseAppVersion, targetAppVersion uint64) {
	t := s.T()

	t.Logf("Upgrading chain from version %d to version %d", baseAppVersion, targetAppVersion)

	// Get validator node and setup tx client
	validatorNode := chain.GetNodes()[0].(*docker.ChainNode)
	cfg := dockerchain.DefaultConfig(s.client, s.network)

	// Get keyring records for all validators
	kr := cfg.Genesis.Keyring()
	records, err := kr.List()
	s.Require().NoError(err, "failed to list keyring records")

	// Signal and execute upgrade
	upgradeHeight := s.signalAndGetUpgradeHeight(ctx, chain, validatorNode, cfg, records, targetAppVersion)

	// Wait for upgrade to complete
	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	status, err := rpcClient.Status(ctx)
	s.Require().NoError(err, "failed to get node status")
	currentHeight := status.SyncInfo.LatestBlockHeight

	blocksToWait := int(upgradeHeight-currentHeight) + 2
	t.Logf("Waiting for %d blocks to reach upgrade height", blocksToWait)
	s.Require().NoError(wait.ForBlocks(ctx, blocksToWait, chain))

	// Verify upgrade
	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(targetAppVersion, abciInfo.Response.GetAppVersion(), "app version mismatch after upgrade")

	t.Logf("Successfully upgraded chain to version %d", targetAppVersion)
}

// createNewChannelAfterUpgrade creates a new IBC channel after the upgrade
func (s *CelestiaTestSuite) createNewChannelAfterUpgrade(ctx context.Context, chainA, chainB tastoratypes.Chain, hermes *relayer.Hermes, connection ibc.Connection) ibc.Channel {
	t := s.T()

	t.Log("Creating new IBC channel after upgrade")

	// Create new channel for token transfers
	channelOpts := ibc.CreateChannelOptions{
		SourcePortName: "transfer",
		DestPortName:   "transfer",
		Order:          ibc.OrderUnordered,
		Version:        "ics20-1",
	}

	channel, err := hermes.CreateChannel(ctx, chainA, connection, channelOpts)
	s.Require().NoError(err, "failed to create new IBC channel after upgrade")

	t.Logf("Successfully created new IBC channel after upgrade: %s", channel.ChannelID)
	return channel
}

// setupSimappTxClient sets up a tx client for the simapp chain
func (s *CelestiaTestSuite) setupSimappTxClient(ctx context.Context, chain tastoratypes.Chain) (*user.TxClient, error) {
	// This is a simplified version - in practice you'd need to implement proper
	// tx client setup for simapp similar to what's done for celestia in dockerchain.SetupTxClient
	return nil, fmt.Errorf("simapp tx client setup not implemented yet")
}

// getBalance gets the balance of a specific denom for an address
func (s *CelestiaTestSuite) getBalance(ctx context.Context, chain tastoratypes.Chain, address, denom string) sdkmath.Int {
	// Query balance using the chain's query client
	// This would need to be implemented based on the chain type
	return sdkmath.ZeroInt()
}

// calculateIBCDenom calculates the IBC denomination for a token transferred over a channel
func (s *CelestiaTestSuite) calculateIBCDenom(channel ibc.Channel, baseDenom string) string {
	// IBC denom format: ibc/{hash of path}
	// Path format: {port}/{channel}/{denom}
	path := fmt.Sprintf("%s/%s/%s", channel.PortID, channel.CounterpartyID, baseDenom)
	// In practice, you'd calculate the SHA256 hash of the path
	// For now, return a placeholder
	return fmt.Sprintf("ibc/%s", path)
}
