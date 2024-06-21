package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
	"github.com/tendermint/tendermint/rpc/client/http"
)

const dynamicTimeoutVersion = "5299334"

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

	err = network.CreateGenesisNodes(4, dynamicTimeoutVersion, 10000000, 0, testnet.DefaultResources)
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
		10,
		"10000-10000",
		1,
		testnet.DefaultResources,
		gRPCEndpoints[0],
	)
	if err != nil {
		return err
	}

	log.Printf("Setting up network\n")
	err = network.Setup(testnet.WithTimeoutCommit(300*time.Millisecond), testnet.WithTimeoutPropose(300*time.Millisecond))
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
	timeout := time.NewTimer(5 * time.Minute)
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
			log.Println("--- PRINTING START TIMES")
			if err := saveStartTimes(network); err != nil {
				return err
			}
			log.Println("--- FINISHED ✅: Dynamic Timeouts")
			return nil
		}
	}
}

func saveStartTimes(testnet *testnet.Testnet) error {
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

	// Create a CSV file
	file, err := os.Create("start_times.csv")
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write headers
	headers := make([]string, len(testnet.Nodes()))
	for i := range headers {
		headers[i] = fmt.Sprintf("Node %d", i)
	}
	if err := writer.Write(headers); err != nil {
		return err
	}

	// Write data for each height
	for height := int64(1); height < earliestLatestHeight; height++ {
		row := make([]string, len(rpcClients))
		for i, client := range rpcClients {
			resp, err := client.StartTime(context.Background(), &height)
			if err != nil {
				row[i] = fmt.Sprintf("Error: %v", err)
				continue
			}
			row[i] = fmt.Sprintf("%d", resp.StartTime.UnixNano())
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return nil
}
