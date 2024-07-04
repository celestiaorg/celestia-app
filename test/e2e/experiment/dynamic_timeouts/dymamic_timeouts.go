package main

import (
	"context"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
	"github.com/tendermint/tendermint/rpc/client/http"
)

const dynamicTimeoutVersion = "e74b11f"

func main() {
	if err := Run(); err != nil {
		log.Fatalf("failed to run experiment: %v", err)
	}
}

func Run() error {
	network, err := testnet.New("dynamic-timeouts", 864, nil, "test")
	if err != nil {
		return err
	}
	defer network.Cleanup()

	err = network.CreateGenesisNodes(2, dynamicTimeoutVersion, 10000000, 0, testnet.DefaultResources)
	if err != nil {
		return err
	}

	gRPCEndpoints, err := network.RemoteGRPCEndpoints()
	if err != nil {
		return err
	}

	err = network.CreateTxClient(
		"txsim",
		dynamicTimeoutVersion,
		1,
		"10000-10000",
		1,
		testnet.DefaultResources,
		gRPCEndpoints[0],
	)
	if err != nil {
		return err
	}

	log.Printf("Setting up network\n")
	err = network.Setup()
	if err != nil {
		return err
	}

	log.Printf("Starting network\n")
	err = network.Start()
	if err != nil {
		return err
	}

	err = network.StartTxClients()
	if err != nil {
		return err
	}

	// run the test for 5 minutes
	ticker := time.NewTicker(5 * time.Second)
	timeout := time.NewTimer(10 * time.Minute)
	rpc := network.Node(0).AddressRPC()
	client, err := http.New(rpc, "/websocket")
	if err != nil {
		return err
	}
	for {
		select {
		case <-ticker.C:
			status, err := client.Status(context.Background())
			if err != nil {
				return err
			}
			log.Printf("Height: %v", status.SyncInfo.LatestBlockHeight)
		case <-timeout.C:
			log.Println("--- FINISHED ✅: Dynamic Timeouts")
			return nil
		}
	}
}
