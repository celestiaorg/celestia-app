package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/celestiaorg/knuu/pkg/knuu"

	"github.com/celestiaorg/celestia-app/v4/test/e2e/testnet"
)

func E2EStateSync(logger *log.Logger) error {
	const (
		testName                   = "E2EStateSync"
		numValidators              = 4
		blocksToProduce            = 30
		stateSyncTrustHeightOffset = 5
		stateSyncTimeout           = 5 * time.Minute
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Knuu
	identifier := fmt.Sprintf("%s_%s", testName, time.Now().Format(timeFormat))
	kn, err := knuu.New(ctx, knuu.Options{
		Scope:        identifier,
		ProxyEnabled: true,
	})
	testnet.NoError("failed to initialize Knuu", err)
	kn.HandleStopSignal(ctx)
	logger.Printf("Knuu initialized with scope %s", kn.Scope)

	testNet, err := testnet.New(logger, kn, testnet.Options{})
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup(ctx)

	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)
	logger.Printf("Running %s test with version %s", testName, latestVersion)

	logger.Println("Creating genesis validator nodes")
	err = testNet.CreateGenesisNodes(ctx, numValidators, latestVersion, 10000000, 0, testnet.DefaultResources, true)
	testnet.NoError("failed to create genesis nodes", err)

	logger.Println("Creating txsim client")
	endpoints, err := testNet.RemoteGRPCEndpoints()
	testnet.NoError("failed to get remote gRPC endpoints", err)
	upgradeSchedule := map[int64]uint64{}
	err = testNet.CreateTxClient(ctx, "txsim", testnet.TxsimVersion, 10, "100-1000", 10, testnet.DefaultResources, endpoints[0], upgradeSchedule)
	testnet.NoError("failed to create tx client", err)

	// Setup Testnet
	logger.Println("Setting up testnet")
	err = testNet.Setup(ctx)
	testnet.NoError("failed to setup testnets", err)

	// Start Testnet (Validators + TxSim)
	logger.Println("Starting initial testnet nodes")
	err = testNet.Start(ctx)
	testnet.NoError("failed to start testnets", err)

	// Wait for blocks to be produced
	logger.Printf("Waiting for %d blocks to be produced", blocksToProduce)
	// Use the first node as a reference
	nodeZeroClient, err := testNet.Node(0).Client()
	testnet.NoError("failed to get client for node 0", err)

	initialHeight := int64(0)
	for i := 0; i < 30; i++ { // Wait up to 30 seconds for the first block
		status, err := nodeZeroClient.Status(ctx)
		if err == nil && status.SyncInfo.LatestBlockHeight > 0 {
			initialHeight = status.SyncInfo.LatestBlockHeight
			break
		}
		time.Sleep(1 * time.Second)
	}
	if initialHeight == 0 {
		return fmt.Errorf("initial nodes failed to produce blocks")
	}

	targetHeight := initialHeight + blocksToProduce
	err = waitForHeight(ctx, nodeZeroClient, targetHeight, 15*time.Second)
	testnet.NoError(fmt.Sprintf("failed to wait for target height %d", targetHeight), err)
	logger.Printf("Reached target height %d", targetHeight)

	logger.Println("Gathering state sync parameters")
	status, err := nodeZeroClient.Status(ctx)
	testnet.NoError("failed to get status from node 0", err)
	latestHeight := status.SyncInfo.LatestBlockHeight
	trustHeight := latestHeight - stateSyncTrustHeightOffset
	if trustHeight <= 0 {
		return fmt.Errorf("calculated trust height %d is too low (latest height: %d)", trustHeight, latestHeight)
	}

	trustBlock, err := nodeZeroClient.Block(ctx, &trustHeight)
	testnet.NoError(fmt.Sprintf("failed to get block at trust height %d", trustHeight), err)
	trustHash := trustBlock.BlockID.Hash.String()

	rpcServers := make([]string, 0, numValidators)
	for i := 0; i < numValidators; i++ {
		rpcAddr, err := testNet.Node(i).RemoteAddressRPC()
		if err != nil {
			return fmt.Errorf("failed to get RPC address for node %d: %w", i, err)
		}
		rpcServers = append(rpcServers, fmt.Sprintf("tcp://%s", rpcAddr))
	}
	stateSyncRPCServers := strings.Join(rpcServers, ",")
	logger.Printf("State sync params: RPCServers=%s, TrustHeight=%d, TrustHash=%s", stateSyncRPCServers, trustHeight, trustHash)

	logger.Println("Creating state sync node")
	stateSyncNodeName := "statesync-node"
	err = testNet.CreateNode(ctx, latestVersion, 0, 0, testnet.DefaultResources, true)
	testnet.NoError("failed to create state sync node", err)
	stateSyncNode := testNet.Nodes()[numValidators]
	stateSyncNode.Name = stateSyncNodeName

	logger.Println("Initializing state sync node")
	peers := make([]string, 0, numValidators)
	for i := 0; i < numValidators; i++ {
		peers = append(peers, testNet.Node(i).AddressP2P(true))
	}

	stateSyncOpt := testnet.WithStateSync(rpcServers, trustHeight, trustHash)
	gendoc, err := testNet.Genesis().Export()
	testnet.NoError("failed to export genesis document", err)
	err = stateSyncNode.Init(ctx, gendoc, peers, stateSyncOpt)
	testnet.NoError("failed to initialize state sync node", err)

	logger.Println("Starting state sync node")
	err = stateSyncNode.Start(ctx)
	testnet.NoError("failed to start state sync node", err)

	logger.Println("Verifying state sync completion")
	stateSyncClient, err := stateSyncNode.Client()
	testnet.NoError("failed to get client for state sync node", err)

	startTime := time.Now()
	for {
		if time.Since(startTime) > stateSyncTimeout {
			return fmt.Errorf("timed out waiting for state sync node to catch up after %v", stateSyncTimeout)
		}

		status, err := stateSyncClient.Status(ctx)
		if err != nil {
			logger.Printf("Failed to get status from state sync node, retrying...: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		logger.Printf("State sync node status: Height=%d, CatchingUp=%t", status.SyncInfo.LatestBlockHeight, status.SyncInfo.CatchingUp)

		if !status.SyncInfo.CatchingUp && status.SyncInfo.LatestBlockHeight >= latestHeight {
			logger.Printf("State sync successful! Node caught up to height %d", status.SyncInfo.LatestBlockHeight)
			break
		}

		time.Sleep(10 * time.Second)
	}

	logger.Println("Verifying synced node continues producing blocks")
	finalTargetHeight := latestHeight + 5
	err = waitForHeight(ctx, stateSyncClient, finalTargetHeight, 15*time.Second)
	testnet.NoError(fmt.Sprintf("state synced node failed to reach height %d", finalTargetHeight), err)
	return nil
}
