package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnets"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

const (
	seed = 42
)

func main() {
	if err := E2EThroughput(); err != nil {
		log.Fatalf("--- ERROR Throughput test: %v", err.Error())
	}
}

func E2EThroughput() error {
	latestVersion, err := testnets.GetLatestVersion()
	testnets.NoError("failed to get latest version", err)

	log.Println("=== RUN E2EThroughput", "version:", latestVersion)

	// create a new testnet
	manifest := testnets.TestManifest{
		ChainID:            "test-chain",
		Validators:         2,
		ValidatorResource:  testnets.DefaultResources,
		TxClientsResource:  testnets.DefaultResources,
		SelfDelegation:     10000000,
		CelestiaAppVersion: latestVersion,
		TxClientVersion:    testnets.TxsimVersion,
		BlobsPerSeq:        1,
		BlobSequences:      1,
		BlobSizes:          "100000",
		PerPeerBandwidth:   5 * 1024 * 1024,
		TimeoutCommit:      1 * time.Second,
		TimeoutPropose:     1 * time.Second,
		Mempool:            "v1",
		BroadcastTxs:       true,
		Prometheus:         true,
		GovMaxSquareSize:   appconsts.DefaultGovMaxSquareSize,
		MaxBlockBytes:      appconsts.DefaultMaxBytes,
	}
	testnet, err := testnets.New("E2EThroughput", seed, testnets.GetGrafanaInfoFromEnvVar(), manifest)
	testnets.NoError("failed to create testnet", err)

	defer func() {
		log.Print("Cleaning up testnet")
		testnet.Cleanup()
	}()

	// add 2 validators
	testnets.NoError("failed to create genesis nodes", testnet.CreateGenesisNodes())

	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := testnet.RemoteGRPCEndpoints()
	testnets.NoError("failed to get validators GRPC endpoints", err)
	log.Println("validators GRPC endpoints", gRPCEndpoints)

	// create txsim nodes and point them to the validators
	log.Println("Creating txsim nodes")

	err = testnet.CreateTxClients(gRPCEndpoints)
	testnets.NoError("failed to create tx clients", err)

	// start the testnet
	log.Println("Setting up testnet")
	testnets.NoError("failed to setup testnet", testnet.Setup())
	log.Println("Starting testnet")
	testnets.NoError("failed to start testnet", testnet.Start())

	// once the testnet is up, start the txsim
	log.Println("Starting txsim nodes")
	testnets.NoError("failed to start tx clients", testnet.StartTxClients())

	// wait some time for the txsim to submit transactions
	time.Sleep(1 * time.Minute)

	log.Println("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(), testnet.Node(0).AddressRPC())
	testnets.NoError("failed to read blockchain", err)

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
