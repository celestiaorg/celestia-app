package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

// This test runs a simple testnet with 4 validators. It submits both MsgPayForBlobs
// and MsgSends over 30 seconds and then asserts that at least 10 transactions were
// committed.
func E2ESimple(logger *log.Logger) error {
	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	logger.Println("Running simple e2e test", "version", latestVersion)

	testNet, err := testnet.New("E2ESimple", seed, nil, "test")
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup()

	logger.Println("Creating testnet validators")
	testnet.NoError("failed to create genesis nodes", testNet.CreateGenesisNodes(4, latestVersion, 10000000, 0, testnet.DefaultResources))

	logger.Println("Creating txsim")
	endpoints, err := testNet.RemoteGRPCEndpoints()
	testnet.NoError("failed to get remote gRPC endpoints", err)
	err = testNet.CreateTxClient("txsim", testnet.TxsimVersion, 10,
		"100-2000", 100, testnet.DefaultResources, endpoints[0])
	testnet.NoError("failed to create tx client", err)

	logger.Println("Setting up testnets")
	testnet.NoError("failed to setup testnets", testNet.Setup())

	logger.Println("Starting testnets")
	testnet.NoError("failed to start testnets", testNet.Start())

	// wait for 30 seconds
	time.Sleep(30 * time.Second)

	logger.Println("Reading blockchain headers")
	blockchain, err := testnode.ReadBlockchainHeaders(context.Background(), testNet.Node(0).AddressRPC())
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
	return nil
}
