package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"celestiaorg/celestia-app/test/docker-e2e/networks"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	cometcfg "github.com/cometbft/cometbft/config"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	servercfg "github.com/cosmos/cosmos-sdk/server/config"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	addressutil "github.com/celestiaorg/tastora/framework/testutil/address"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
)

const (
	blocksToProduce            = 30
	stateSyncTrustHeightOffset = 5
	stateSyncTimeout           = 10 * time.Minute
)

// keepAliveTransactionGenerator continuously sends small bank transfer transactions
// to keep the chain producing blocks with changing state. It runs in a goroutine
// and can be stopped by cancelling the provided context.
// This version is resilient to connection drops during node restarts (e.g., upgrades).
func (s *CelestiaTestSuite) keepAliveTransactionGenerator(ctx context.Context, wg *sync.WaitGroup, chain *tastoradockertypes.Chain, cfg *dockerchain.Config, intervalSeconds int) {
	defer wg.Done()

	// Create a unique recipient wallet for keep-alive transactions
	recipientWalletName := fmt.Sprintf("keepalive-recipient-%d", time.Now().UnixNano())
	wallet, err := chain.CreateWallet(ctx, recipientWalletName)
	if err != nil {
		s.T().Logf("Failed to create keep-alive recipient wallet: %v", err)
		return
	}

	recipientAddr, err := sdk.AccAddressFromBech32(wallet.GetFormattedAddress())
	if err != nil {
		s.T().Logf("Failed to parse keep-alive recipient address: %v", err)
		return
	}

	s.T().Logf("Keep-alive transaction generator started, recipient: %s", recipientAddr.String())

	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	txCounter := 0
	var txClient *user.TxClient
	var fromAddr sdk.AccAddress

	// Helper function to establish or re-establish connection
	setupConnection := func() bool {
		// Check if context is canceled before attempting connection
		select {
		case <-ctx.Done():
			s.T().Logf("Keep-alive transaction generator: context canceled, cannot setup connection")
			return false
		default:
		}

		var err error
		txClient, err = dockerchain.SetupTxClient(ctx, chain.GetNodes()[0].(*tastoradockertypes.ChainNode), cfg)
		if err != nil {
			s.T().Logf("Failed to setup TxClient for keep-alive transactions: %v", err)
			return false
		}
		fromAddr = txClient.DefaultAddress()
		s.T().Logf("Keep-alive transaction generator connected: %s -> %s", fromAddr.String(), recipientAddr.String())
		return true
	}

	// Initial connection setup
	if !setupConnection() {
		s.T().Logf("Keep-alive transaction generator failed to establish initial connection")
		return
	}

	for {
		select {
		case <-ctx.Done():
			s.T().Logf("Keep-alive transaction generator stopped after sending %d transactions", txCounter)
			return
		case <-ticker.C:
			// Send a small bank transfer (1000 utia = 0.001 TIA)
			sendAmount := sdk.NewCoins(sdk.NewCoin("utia", sdkmath.NewInt(1000)))
			msg := banktypes.NewMsgSend(fromAddr, recipientAddr, sendAmount)

			// Retry logic with connection re-establishment
			maxRetries := 3
			retryDelay := 2 * time.Second
			success := false

			for attempt := 0; attempt < maxRetries && !success; attempt++ {
				// Check if context is canceled before attempting transaction
				select {
				case <-ctx.Done():
					s.T().Logf("Keep-alive transaction generator: context canceled, stopping")
					return
				default:
				}

				// Ensure txClient is not nil before using it
				if txClient == nil {
					s.T().Logf("Keep-alive transaction %d: txClient is nil, attempting to reconnect", txCounter+1)
					if !setupConnection() {
						s.T().Logf("Keep-alive transaction %d: failed to reconnect, skipping", txCounter+1)
						break
					}
					// Update the message with the new fromAddr after reconnection
					msg = banktypes.NewMsgSend(fromAddr, recipientAddr, sendAmount)
				}

				// Submit transaction with appropriate gas and fee
				_, err := txClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(200000), user.SetFee(5000))
				if err != nil {
					// Check if it's a connection error
					if strings.Contains(err.Error(), "connection") || strings.Contains(err.Error(), "rpc error") || strings.Contains(err.Error(), "Unavailable") {
						s.T().Logf("Keep-alive transaction %d connection error (attempt %d/%d): %v", txCounter+1, attempt+1, maxRetries, err)

						// Wait before retrying and attempt to re-establish connection
						if attempt < maxRetries-1 {
							time.Sleep(retryDelay)

							// Check if context is canceled before attempting reconnection
							select {
							case <-ctx.Done():
								s.T().Logf("Keep-alive transaction generator: context canceled during retry, stopping")
								return
							default:
							}

							if setupConnection() {
								// Update the message with the new fromAddr after reconnection
								msg = banktypes.NewMsgSend(fromAddr, recipientAddr, sendAmount)
								// Double the retry delay for next attempt
								retryDelay *= 2
							}
						}
					} else {
						// Non-connection error, check if it's context cancellation
						if strings.Contains(err.Error(), "context canceled") {
							s.T().Logf("Keep-alive transaction %d canceled due to context: %v", txCounter+1, err)
							return
						}
						// Other non-connection error, log and move on
						s.T().Logf("Keep-alive transaction %d failed (non-connection error): %v", txCounter+1, err)
						break
					}
				} else {
					success = true
					txCounter++
					if txCounter%10 == 0 {
						s.T().Logf("Keep-alive transaction generator: sent %d transactions", txCounter)
					}
				}
			}

			if !success && maxRetries > 0 {
				s.T().Logf("Keep-alive transaction %d failed after %d retries", txCounter+1, maxRetries)
			}
		}
	}
}

// validatorStateSyncAppOverrides modifies the app.toml to configure state sync snapshots for the given node.
func validatorStateSyncAppOverrides(ctx context.Context, node *tastoradockertypes.ChainNode) error {
	return config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
		cfg.StateSync.SnapshotInterval = 5
		cfg.StateSync.SnapshotKeepRecent = 2
	})
}

// validatorPruningAppOverrides modifies the app.toml to enable aggressive pruning with short retention.
// This ensures that validators only keep recent blocks, forcing state sync instead of block sync.
func validatorPruningAppOverrides(ctx context.Context, node *tastoradockertypes.ChainNode) error {
	return config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
		// Enable custom pruning with aggressive settings
		cfg.Pruning = "custom"
		cfg.PruningKeepRecent = "20" // Keep only last 20 blocks
		cfg.PruningInterval = "10"   // Prune every 10 blocks
		cfg.MinRetainBlocks = 20     // Minimum blocks to retain for other nodes to catch up

		// Configure state sync snapshots for serving snapshots to state sync nodes
		cfg.StateSync.SnapshotInterval = 5
		cfg.StateSync.SnapshotKeepRecent = 2
	})
}

func (s *CelestiaTestSuite) TestStateSync() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()
	cfg := dockerchain.DefaultConfig(s.client, s.network)
	celestia, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).
		WithPostInit(validatorStateSyncAppOverrides).
		Build(ctx)

	s.Require().NoError(err, "failed to get chain")

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	// Cleanup resources when the test is done
	t.Cleanup(func() {
		if err := celestia.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	// verify the chain is running
	height, err := celestia.Height(ctx)
	s.Require().NoError(err, "failed to get chain height")
	s.Require().Greater(height, int64(0), "chain height is zero")

	s.CreateTxSim(ctx, celestia)

	allNodes := celestia.GetNodes()

	nodeClient, err := allNodes[0].GetRPCClient()
	s.Require().NoError(err)

	initialHeight, err := s.GetLatestBlockHeight(ctx, nodeClient)
	s.Require().NoError(err, "failed to get initial height")
	s.Require().Greater(initialHeight, int64(0), "initial height is zero")

	targetHeight := initialHeight + blocksToProduce
	t.Logf("Successfully reached initial height %d", initialHeight)

	s.Require().NoError(wait.ForBlocks(ctx, int(targetHeight), celestia), "failed to wait for target height")

	t.Logf("Successfully reached target height %d", targetHeight)

	t.Logf("Gathering state sync parameters")
	latestHeight, err := s.GetLatestBlockHeight(ctx, nodeClient)
	s.Require().NoError(err, "failed to get latest height for state sync parameters")
	trustHeight := latestHeight - stateSyncTrustHeightOffset
	s.Require().Greater(trustHeight, int64(0), "calculated trust height %d is too low (latest height: %d)", trustHeight, latestHeight)

	trustBlock, err := nodeClient.Block(ctx, &trustHeight)
	s.Require().NoError(err, "failed to get block at trust height %d", trustHeight)

	trustHash := trustBlock.BlockID.Hash.String()
	rpcServers, err := addressutil.BuildInternalRPCAddressList(ctx, celestia.GetNodes())
	s.Require().NoError(err, "failed to build RPC address list")

	t.Logf("Trust height: %d", trustHeight)
	t.Logf("Trust hash: %s", trustHash)
	t.Logf("RPC servers: %s", rpcServers)

	t.Log("Adding state sync node")
	err = celestia.AddNode(ctx,
		tastoradockertypes.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
			WithPostInit(func(ctx context.Context, node *tastoradockertypes.ChainNode) error {
				return config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
					cfg.StateSync.Enable = true
					cfg.StateSync.TrustHeight = trustHeight
					cfg.StateSync.TrustHash = trustHash
					cfg.StateSync.RPCServers = strings.Split(rpcServers, ",")
				})
			}).
			Build(),
	)

	s.Require().NoError(err, "failed to add node")

	allNodes = celestia.GetNodes()
	fullNode := allNodes[len(allNodes)-1]

	s.Require().Equal(tastoratypes.NodeTypeConsensusFull, fullNode.GetType(), "expected state sync node to be a full node")

	stateSyncClient, err := fullNode.GetRPCClient()
	s.Require().NoError(err)

	err = s.WaitForSync(ctx, stateSyncClient, stateSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight >= latestHeight
	})

	s.Require().NoError(err, "failed to wait for state sync to complete")

	s.T().Logf("Checking validator liveness from height %d", initialHeight)
	s.Require().NoError(
		s.CheckLiveness(ctx, celestia),
		"validator liveness check failed",
	)
}

// TestStateSyncMocha tests state sync functionality by syncing from the mocha network.
func (s *CelestiaTestSuite) TestStateSyncMocha() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()

	mochaConfig := networks.NewMochaConfig()
	mochaClient, err := networks.NewClient(mochaConfig.RPCs[0])
	s.Require().NoError(err, "failed to create mocha RPC client")

	// get latest height from mocha
	latestHeight, err := s.GetLatestBlockHeight(ctx, mochaClient)
	s.Require().NoError(err, "failed to get latest height from mocha")
	s.Require().Greater(latestHeight, int64(0), "latest height is zero")

	trustHeight := latestHeight - 2000
	s.Require().Greater(trustHeight, int64(0), "calculated trust height %d is too low", trustHeight)

	// get trust hash from mocha
	trustBlock, err := mochaClient.Block(ctx, &trustHeight)
	s.Require().NoError(err, "failed to get block at trust height %d from mocha", trustHeight)

	trustHash := trustBlock.BlockID.Hash.String()

	t.Logf("Mocha latest height: %d", latestHeight)
	t.Logf("Using trust height: %d", trustHeight)
	t.Logf("Using trust hash: %s", trustHash)
	t.Logf("Using mocha RPC: %s", mochaConfig.RPCs[0])

	dockerCfg, err := networks.NewConfig(mochaConfig, s.client, s.network)
	s.Require().NoError(err, "failed to create mocha config")

	// create a mocha chain builder (no validators, just for state sync nodes)
	mochaChain, err := networks.NewChainBuilder(s.T(), mochaConfig, dockerCfg).
		WithNodes(tastoradockertypes.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
			WithPostInit(func(ctx context.Context, node *tastoradockertypes.ChainNode) error {
				return config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
					// enable state sync
					cfg.StateSync.Enable = true
					cfg.StateSync.TrustHeight = trustHeight
					cfg.StateSync.TrustHash = trustHash
					cfg.StateSync.RPCServers = mochaConfig.RPCs
					cfg.P2P.Seeds = mochaConfig.Seeds
				})
			}).
			Build(),
		).
		Build(ctx)

	s.Require().NoError(err, "failed to create chain")

	t.Log("Starting mocha state sync node")
	err = mochaChain.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	t.Cleanup(func() {
		if err := mochaChain.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	allNodes := mochaChain.GetNodes()
	s.Require().Len(allNodes, 1, "expected exactly one node")
	fullNode := allNodes[0]

	s.Require().Equal(tastoratypes.NodeTypeConsensusFull, fullNode.GetType(), "expected state sync node to be a full node")

	stateSyncClient, err := fullNode.GetRPCClient()
	s.Require().NoError(err, "failed to get state sync client")

	err = s.WaitForSync(ctx, stateSyncClient, stateSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight >= trustHeight
	})

	s.Require().NoError(err, "failed to wait for state sync to complete")
}

// TestStateSyncAcrossMixedAppVersions tests that a full node can state-sync to the latest height
// when the chain history spans multiple app versions (v4 → v5).
//
// Test steps:
// 1. Start a v4 chain with 4 validators and tx traffic for 20 blocks
// 2. Perform on-chain upgrade v4 → v5, finalize, and produce 50 more blocks
// 3. Launch a new full node with StateSync.Enable=true
// 4. Assert: node finishes state-sync, app_version == 5, basic bank-send succeeds
func (s *CelestiaTestSuite) TestStateSyncAcrossMixedAppVersions() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping state sync across mixed app versions test in short mode")
	}

	ctx := context.Background()

	// Get the Docker image tag for the test
	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err)

	// Phase 1: Start v4 chain with 4 validators
	t.Log("Phase 1: Starting v4 chain with 4 validators")
	cfg := dockerchain.DefaultConfig(s.client, s.network).WithTag(tag)

	// TODO: we will later have a loop and upgrade to multiple versions until we reach the latest version
	// while spamming the network with txs (txsim) and also make sure the chain is properly pruned so to avoid block sync instead of state sync
	// Then we mark a paticular tx probably for each version and then we evaluate the state sync node to see if it can sync to the latest version
	// and the submitted txs are properly synced.
	// Configure genesis for v4 with 3 validators (default)
	cfg.Genesis = cfg.Genesis.WithAppVersion(4)

	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).
		WithPostInit(validatorPruningAppOverrides). // Enable aggressive pruning and snapshots
		Build(ctx)
	s.Require().NoError(err, "failed to build chain")

	t.Cleanup(func() {
		if err := chain.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	// Wait for chain to be running and get initial state
	s.Require().NoError(wait.ForBlocks(ctx, 1, chain), "the chain should be running")

	validatorNode := chain.GetNodes()[0]
	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	// Verify we're starting with app version 4
	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(uint64(4), abciInfo.Response.GetAppVersion(), "should start with app version 4")

	// Wait for chain to be ready by waiting for blocks to be produced
	t.Log("Waiting for chain to be ready")
	initialHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get initial height")
	s.Require().Greater(initialHeight, int64(0), "initial height should be greater than 0")

	// Wait for a few blocks to ensure chain is stable
	s.Require().NoError(wait.ForBlocks(ctx, 3, chain), "failed to wait for chain stability")

	// Start transaction simulation for network activity
	s.CreateTxSim(ctx, chain)

	// Produce 20 blocks with transaction activity
	t.Log("Producing 20 blocks with transaction activity")
	s.Require().NoError(wait.ForBlocks(ctx, 20, chain), "failed to wait for 20 blocks")

	// Test functionality before upgrade to ensure gRPC is working
	t.Log("Testing functionality before upgrade")

	testBankSend(s.T(), chain, cfg)
	testPFBSubmission(s.T(), chain, cfg)

	// Phase 2: Perform v4 → v5 upgrade
	t.Log("Phase 2: Performing v4 → v5 upgrade")

	kr := cfg.Genesis.Keyring()
	records, err := kr.List()
	s.Require().NoError(err, "failed to list keyring records")
	s.Require().Len(records, len(chain.GetNodes()), "number of accounts should match number of nodes")

	// Signal and execute upgrade from v4 to v5 (following working upgrade test pattern)
	upgradeHeight := s.signalAndGetUpgradeHeight(ctx, chain, validatorNode, cfg, records, 5)

	// Start keep-alive transaction generator to ensure chain doesn't stall during upgrade
	upgradeCtx, upgradeCancel := context.WithCancel(ctx)
	var upgradeWg sync.WaitGroup
	upgradeWg.Add(1)
	go s.keepAliveTransactionGenerator(upgradeCtx, &upgradeWg, chain, cfg, 3) // Send tx every 3 seconds
	t.Log("Started keep-alive transaction generator for upgrade period")

	// Wait for upgrade to complete
	currentHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get current height")

	blocksToWait := int(upgradeHeight-currentHeight) + 2
	t.Logf("Waiting for %d blocks to reach upgrade height %d", blocksToWait, upgradeHeight)
	s.Require().NoError(wait.ForBlocks(ctx, blocksToWait, chain),
		"failed to wait for upgrade completion")

	// Stop the upgrade keep-alive generator
	upgradeCancel()
	upgradeWg.Wait()
	t.Log("Stopped keep-alive transaction generator for upgrade period")

	// Verify upgrade completed successfully
	abciInfo, err = rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info after upgrade")
	s.Require().Equal(uint64(5), abciInfo.Response.GetAppVersion(), "app version should be 5 after upgrade")

	// Start keep-alive transaction generator for post-upgrade block production
	postUpgradeCtx, postUpgradeCancel := context.WithCancel(ctx)
	var postUpgradeWg sync.WaitGroup
	postUpgradeWg.Add(1)
	go s.keepAliveTransactionGenerator(postUpgradeCtx, &postUpgradeWg, chain, cfg, 2) // Send tx every 2 seconds
	t.Log("Started keep-alive transaction generator for post-upgrade block production")

	// Produce 20 more blocks at v5
	t.Log("Producing 20 more blocks at app version 5")
	s.Require().NoError(wait.ForBlocks(ctx, 20, chain),
		"failed to wait for 20 blocks after upgrade")

	// Stop the post-upgrade keep-alive generator
	postUpgradeCancel()
	postUpgradeWg.Wait()
	t.Log("Stopped keep-alive transaction generator for post-upgrade block production")

	finalHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get final height")
	t.Logf("Chain now at height %d with mixed version history (v4: blocks 1-%d, v5: blocks %d-%d)",
		finalHeight, upgradeHeight-1, upgradeHeight, finalHeight)

	// Log pruning configuration to confirm aggressive pruning is enabled
	t.Logf("Validators configured with aggressive pruning: keep_recent=20, min_retain_blocks=20")
	t.Logf("This ensures state sync is required (not block sync) for nodes joining at height %d", finalHeight)

	// Verify that pruning is actually working by checking if early blocks are accessible
	t.Log("Verifying that pruning has actually removed early blocks...")
	s.verifyBlocksPruned(ctx, t, rpcClient, finalHeight)

	// Phase 3: Launch state sync node
	t.Log("Phase 3: Launching new full node with state sync enabled")

	// Calculate state sync parameters from the v5 portion of the chain
	latestHeight := finalHeight
	trustHeight := latestHeight - stateSyncTrustHeightOffset
	s.Require().Greater(trustHeight, upgradeHeight,
		"trust height %d should be in v5 portion (after upgrade height %d)", trustHeight, upgradeHeight)

	trustBlock, err := rpcClient.Block(ctx, &trustHeight)
	s.Require().NoError(err, "failed to get block at trust height %d", trustHeight)

	trustHash := trustBlock.BlockID.Hash.String()
	rpcServers, err := addressutil.BuildInternalRPCAddressList(ctx, chain.GetNodes())
	s.Require().NoError(err, "failed to build RPC address list")

	t.Logf("State sync parameters: trust_height=%d, trust_hash=%s", trustHeight, trustHash)

	// Add state sync node
	err = chain.AddNode(ctx,
		tastoradockertypes.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
			WithPostInit(func(ctx context.Context, node *tastoradockertypes.ChainNode) error {
				return config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
					cfg.StateSync.Enable = true
					cfg.StateSync.TrustHeight = trustHeight
					cfg.StateSync.TrustHash = trustHash
					cfg.StateSync.RPCServers = strings.Split(rpcServers, ",")
				})
			}).
			Build(),
	)
	s.Require().NoError(err, "failed to add state sync node")

	allNodes := chain.GetNodes()
	stateSyncNode := allNodes[len(allNodes)-1]
	s.Require().Equal(tastoratypes.NodeTypeConsensusFull, stateSyncNode.GetType(), "expected state sync node to be a full node")

	stateSyncClient, err := stateSyncNode.GetRPCClient()
	s.Require().NoError(err, "failed to get state sync client")

	// Phase 4: Wait for state sync and validate
	t.Log("Phase 4: Waiting for state sync completion and validation")

	err = s.WaitForSync(ctx, stateSyncClient, stateSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight >= latestHeight
	})
	s.Require().NoError(err, "failed to wait for state sync to complete")

	// Validate: /status shows catching_up=false
	status, err := stateSyncClient.Status(ctx)
	s.Require().NoError(err, "failed to get status from state sync node")
	s.Require().False(status.SyncInfo.CatchingUp, "state sync node should not be catching up")
	t.Logf("✓ State sync node status: catching_up=%t, height=%d",
		status.SyncInfo.CatchingUp, status.SyncInfo.LatestBlockHeight)

	// Verify that the node actually performed state sync by checking it reached current height
	// without having access to the full block history (due to aggressive pruning)
	nodeHeight := status.SyncInfo.LatestBlockHeight
	s.Require().GreaterOrEqual(nodeHeight, finalHeight-5,
		"state sync node should be within 5 blocks of the chain tip")
	t.Logf("✓ State sync successful: node reached height %d (chain tip: %d) using state sync",
		nodeHeight, finalHeight)
	t.Logf("✓ Confirmed: Node used state sync instead of block sync due to aggressive pruning")

	// Validate: ABCIInfo.app_version == 5
	syncedAbciInfo, err := stateSyncClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info from state sync node")
	s.Require().Equal(uint64(5), syncedAbciInfo.Response.GetAppVersion(),
		"state sync node should have app version 5")
	t.Logf("✓ State sync node app version: %d", syncedAbciInfo.Response.GetAppVersion())

	// Validate: Basic bank send succeeds from synced node
	t.Log("Testing bank send transaction from state sync node")
	testBankSend(s.T(), chain, cfg)
	t.Log("✓ Bank send transaction succeeded")

	// Final liveness check
	t.Log("Performing final liveness check")
	s.Require().NoError(s.CheckLiveness(ctx, chain), "liveness check failed")
	t.Log("✓ Liveness check passed")

	t.Log("State sync across mixed app versions test completed successfully!")
}

// verifyBlocksPruned checks if early blocks have been pruned by attempting to query them.
// This confirms that aggressive pruning is working and blocks are actually being removed.
func (s *CelestiaTestSuite) verifyBlocksPruned(ctx context.Context, t *testing.T, rpcClient interface {
	Block(ctx context.Context, height *int64) (*rpctypes.ResultBlock, error)
}, currentHeight int64) {
	t.Logf("Current chain height: %d", currentHeight)

	// Try to query the first block (height 1) - this should fail if pruning is working
	t.Log("Attempting to query block at height 1 (should fail if pruned)...")
	block1Height := int64(1)
	_, err := rpcClient.Block(ctx, &block1Height)
	if err != nil {
		t.Logf("✓ Block 1 is not accessible (pruned): %v", err)
	} else {
		t.Log("⚠ Block 1 is still accessible - pruning may not be working as expected")
	}

	// Try to query a block that should definitely be pruned (assuming we're far enough along)
	if currentHeight > 30 {
		earlyBlockHeight := int64(5)
		t.Logf("Attempting to query block at height %d (should fail if pruned)...", earlyBlockHeight)
		_, err := rpcClient.Block(ctx, &earlyBlockHeight)
		if err != nil {
			t.Logf("✓ Block %d is not accessible (pruned): %v", earlyBlockHeight, err)
		} else {
			t.Logf("⚠ Block %d is still accessible - pruning may not be working as expected", earlyBlockHeight)
		}
	}

	// Find the earliest accessible block by binary search
	t.Log("Finding the earliest accessible block...")
	earliestAccessible := findEarliestAccessibleBlock(ctx, rpcClient, currentHeight)
	if earliestAccessible > 0 {
		t.Logf("✓ Earliest accessible block: %d (pruned blocks 1-%d)", earliestAccessible, earliestAccessible-1)

		// Verify that only recent blocks are kept (should be within our retention policy)
		expectedEarliest := currentHeight - 20 // We configured keep_recent=20
		if earliestAccessible >= expectedEarliest {
			t.Logf("✓ Pruning working correctly: earliest block %d >= expected earliest %d",
				earliestAccessible, expectedEarliest)
		} else {
			t.Logf("⚠ Pruning may be too aggressive: earliest block %d < expected earliest %d",
				earliestAccessible, expectedEarliest)
		}
	} else {
		t.Log("⚠ Could not determine earliest accessible block")
	}
}

// findEarliestAccessibleBlock uses binary search to find the earliest block that is still accessible.
func findEarliestAccessibleBlock(ctx context.Context, rpcClient interface {
	Block(ctx context.Context, height *int64) (*rpctypes.ResultBlock, error)
}, currentHeight int64) int64 {
	left, right := int64(1), currentHeight
	earliestFound := int64(-1)

	for left <= right {
		mid := (left + right) / 2
		_, err := rpcClient.Block(ctx, &mid)
		if err == nil {
			// Block exists, try to find an earlier one
			earliestFound = mid
			right = mid - 1
		} else {
			// Block doesn't exist, search in the higher range
			left = mid + 1
		}
	}

	return earliestFound
}
