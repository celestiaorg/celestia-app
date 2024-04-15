package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnets"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

var latestVersion = "latest"

const seed = 42

func main() {
	if err := E2EThroughput(); err != nil {
		log.Fatalf("--- ERROR Throughput test: %v", err.Error())
	}
}

func E2EThroughput() error {
	log.SetFlags(0) // Disable additional information like the date or time
	log.SetPrefix("    ")

	if os.Getenv("E2E_LATEST_VERSION") != "" {
		latestVersion = os.Getenv("E2E_LATEST_VERSION")
		_, isSemVer := testnets.ParseVersion(latestVersion)
		switch {
		case isSemVer:
		case latestVersion == "latest":
		case len(latestVersion) == 7:
		case len(latestVersion) >= 8:
			// assume this is a git commit hash (we need to trim the last digit to match the docker image tag)
			latestVersion = latestVersion[:7]
		default:
			return fmt.Errorf("unrecognised version %s", latestVersion)
		}
	}

	log.Println("=== RUN E2EThroughput", "version:", latestVersion)

	// create a new testnet
	testnet, err := testnets.New("E2EThroughput", seed, testnets.GetGrafanaInfoFromEnvVar())
	testnets.NoError("failed to create testnet", err)

	log.Println("Cleaning up testnet")
	defer testnet.Cleanup()

	// add 2 validators
	testnets.NoError("failed to create genesis nodes", testnet.CreateGenesisNodes(2, latestVersion, 10000000, 0, testnets.DefaultResources))

	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := testnet.RemoteGRPCEndpoints()
	testnets.NoError("failed to get validators GRPC endpoints", err)
	log.Println("validators GRPC endpoints", gRPCEndpoints)

	// create txsim nodes and point them to the validators
	log.Println("Creating txsim nodes")
	// version of the txsim docker image to be used
	txsimVersion := "a92de72"

	err = testnet.CreateTxClients(txsimVersion, 1, "10000-10000", testnets.DefaultResources, gRPCEndpoints)
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
