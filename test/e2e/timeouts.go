package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
)

// Timeouts runs a simple testnet with 4 validators. It submits both MsgPayForBlobs
// and MsgSends over 30 seconds and then asserts that at least 10 transactions were
// committed.
func Timeouts(logger *log.Logger) error {
	ctx := context.Background()
	testNet, err := testnet.New(ctx, "Timeouts", seed, nil, "test")
	testnet.NoError("failed to create testnet", err)

	logger.Println("Genesis app version is", testNet.GetGenesisAppVersion())
	defer testNet.Cleanup(ctx)

	latestVersion := "pr-3882"
	testnet.NoError("failed to get latest version", err)

	logger.Println("Running Timeouts test", "version", latestVersion)

	logger.Println("Creating testnet validators")
	testnet.NoError("failed to create genesis nodes",
		testNet.CreateGenesisNodes(ctx, 4, latestVersion, 10000000, 0, testnet.DefaultResources, true))

	logger.Println("Creating txsim")
	endpoints, err := testNet.RemoteGRPCEndpoints(ctx)
	testnet.NoError("failed to get remote gRPC endpoints", err)
	err = testNet.CreateTxClient(ctx, "txsim", latestVersion, 60,
		"20000", 6, testnet.DefaultResources, endpoints[0])
	testnet.NoError("failed to create tx client", err)

	logger.Println("Setting up testnets")
	// We set the timeouts intentionally far from the default values to make sure
	// that the configs do not take effect rather timeouts are set based on the
	// app version in the genesis or the block headers
	testnet.NoError("failed to setup testnets", testNet.Setup(ctx,
		testnet.WithTimeoutPropose(1*time.Second),
		testnet.WithTimeoutCommit(30*time.Second)))

	logger.Println("Starting testnets")
	// only start 3/4 of the nodes
	testnet.NoError("failed to start testnets", testNet.Start(ctx, 0, 1, 2, 3))

	logger.Println("Waiting for some time to produce blocks")
	time.Sleep(60 * time.Second)
	//
	//// now start the last node
	//testNet.Start(ctx, 3)
	//
	//logger.Println("Waiting for some time  for the last node to catch" +
	//	" up and" +
	//	" produce blocks")
	//time.Sleep(120 * time.Second)
	// TODO can extend the test by turning off one of the nodes and checking if the network still works

	logger.Println("Reading blockchain headers")
	blockchain, err := testnode.ReadBlockchainHeaders(ctx, testNet.Node(0).AddressRPC())
	testnet.NoError("failed to read blockchain headers", err)

	totalTxs := 0
	for _, blockMeta := range blockchain {
		version := blockMeta.Header.Version.App
		if appconsts.LatestVersion != version {
			return fmt.Errorf("expected app version %d, got %d in blockMeta %d", appconsts.LatestVersion, version, blockMeta.Header.Height)
		}
		totalTxs += blockMeta.NumTxs
	}
	if totalTxs < 10 {
		return fmt.Errorf("expected at least 10 transactions, got %d", totalTxs)
	}
	blockTimes := testnode.CalculateBlockTime(blockchain)
	stats := testnode.CalculateStats(blockTimes)
	targetBlockTime := 6 * time.Second
	targetBlockTimeIsReached := (stats.Avg < targetBlockTime+1*time.Second) &&
		(stats.Avg > targetBlockTime-1*time.Second)
	if !targetBlockTimeIsReached {
		return fmt.Errorf("expected block time to be around %v+-1s, got %v",
			targetBlockTime, stats.Avg)
	}
	return nil
}
