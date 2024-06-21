package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
	"github.com/tendermint/tendermint/rpc/client/http"
)

const dynamicTimeoutVersion = "4c8ce77"

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
	err = network.Setup(testnet.WithTimeoutCommit(time.Second))
	if err != nil {
		return err
	}

	log.Printf("Starting network\n")
	err = network.Start()
	if err != nil {
		return err
	}

	// run the test for 5 minutes
	ticker := time.NewTicker(10 * time.Second)
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
			if err := printStartTimes(network); err != nil {
				return err
			}
			log.Println("--- FINISHED ✅: Dynamic Timeouts")
			return nil
		}
	}
}

func printStartTimes(testnet *testnet.Testnet) error {
	rpcClients := make([]*http.HTTP, len(testnet.Nodes()))
	earliestLatestHeight := int64(0)
	for i, node := range testnet.Nodes() {
		client, err := node.Client()
		if err != nil {
			return err
		}
		rpcClients[i] = client
		status, err := client.Status(context.Background())
		if err != nil {
			return err
		}
		if earliestLatestHeight == 0 || earliestLatestHeight < status.SyncInfo.LatestBlockHeight {
			earliestLatestHeight = status.SyncInfo.LatestBlockHeight
		}
	}
	for height := int64(0); height < earliestLatestHeight; height++ {
		fmt.Println("Height: ", height)
		for i, client := range rpcClients {
			resp, err := client.StartTime(context.Background(), &height)
			if err != nil {
				fmt.Printf("Error getting start time for node %d: %v\n", i, err)
				continue
			}
			fmt.Printf("Node %d started at %v\n", i, resp.StartTime)
		}
	}
	return nil
}
