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

const (
	seed         = 42
	txsimVersion = "a92de72"
)

func main() {
	if err := E2EThroughput(); err != nil {
		log.Fatalf("--- ERROR Throughput test: %v", err.Error())
	}
}

func E2EThroughput() error {
	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	log.Println("=== RUN E2EThroughput", "version:", latestVersion)

	// create a new testnet
	testNet, err := testnet.New("E2EThroughput", seed, testnet.GetGrafanaInfoFromEnvVar())
	testnet.NoError("failed to create testnet", err)

	defer func() {
		log.Print("Cleaning up testnet")
		testNet.Cleanup()
	}()

	// add 2 validators
	testnet.NoError("failed to create genesis nodes", testNet.CreateGenesisNodes(2, latestVersion, 10000000, 0, testnet.DefaultResources))

	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := testNet.RemoteGRPCEndpoints()
	testnet.NoError("failed to get validators GRPC endpoints", err)
	log.Println("validators GRPC endpoints", gRPCEndpoints)

	// create txsim nodes and point them to the validators
	log.Println("Creating txsim nodes")

	err = testNet.CreateTxClients(txsimVersion, 1, "10000-10000", testnet.DefaultResources, gRPCEndpoints)
	testnet.NoError("failed to create tx clients", err)

	// start the testnet
	log.Println("Setting up testnet")
	testnet.NoError("failed to setup testnet", testNet.Setup())
	log.Println("Starting testnet")
	testnet.NoError("failed to start testnet", testNet.Start())

	// once the testnet is up, start the txsim
	log.Println("Starting txsim nodes")
	testnet.NoError("failed to start tx clients", testNet.StartTxClients())

	// wait some time for the txsim to submit transactions
	time.Sleep(1 * time.Minute)

	log.Println("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(), testNet.Node(0).AddressRPC())
	testnet.NoError("failed to read blockchain", err)

	totalTxs := 0
	for _, block := range blockchain {
		if appconsts.LatestVersion != block.Version.App {
			return fmt.Errorf("expected app version %d, got %d", appconsts.LatestVersion, block.Version.App)
		}
		totalTxs += len(block.Data.Txs)
	}
	if totalTxs < 10 {
		return fmt.Errorf("expected at least 10 transactions, got %d", totalTxs)
	}
	log.Println("--- PASS ✅: E2EThroughput")
	return nil
}
