package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/knuu/pkg/knuu"

	"github.com/celestiaorg/celestia-app/v4/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
)

// E2ESimple runs a simple testnet with 4 validators. It submits both MsgPayForBlobs
// and MsgSends over 30 seconds and then asserts that at least 10 transactions were
// committed.
func E2ESimple(logger *log.Logger) error {
	const testName = "E2ESimple"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	logger.Println("Creating testnet validators")
	testnet.NoError("failed to create genesis nodes",
		testNet.CreateGenesisNodes(ctx, 4, latestVersion, 10000000, 0, testnet.DefaultResources, true))

	logger.Println("Creating txsim")
	endpoints, err := testNet.RemoteGRPCEndpoints()
	testnet.NoError("failed to get remote gRPC endpoints", err)
	upgradeSchedule := map[int64]uint64{}
	err = testNet.CreateTxClient(ctx, "txsim", testnet.TxsimVersion, 10, "100-2000", 100, testnet.DefaultResources, endpoints[0], upgradeSchedule)
	testnet.NoError("failed to create tx client", err)

	logger.Println("Setting up testnets")
	testnet.NoError("failed to setup testnets", testNet.Setup(ctx, testnet.WithPrometheus(false))) // TODO: re-enable prometheus once fixed in comet

	logger.Println("Starting testnets")
	testnet.NoError("failed to start testnets", testNet.Start(ctx))

	logger.Println("Waiting for 30 seconds to produce blocks")
	time.Sleep(30 * time.Second)

	logger.Println("Reading blockchain headers")
	blockchain, err := testnode.ReadBlockchainHeaders(ctx, testNet.Node(0).AddressRPC())
	testnet.NoError("failed to read blockchain headers", err)

	totalTxs := 0
	for _, blockMeta := range blockchain {
		totalTxs += blockMeta.NumTxs
	}
	if totalTxs < 10 {
		return fmt.Errorf("expected at least 10 transactions, got %d", totalTxs)
	}
	return nil
}
