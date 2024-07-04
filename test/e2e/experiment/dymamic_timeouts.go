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

const (
	dynamicTimeoutVersion = "5299334"
	staticTimeoutVersion  = "9fdd971"
)

func main() {
	if err := Run(); err != nil {
		log.Fatalf("failed to run experiment: %v", err)
	}
}

func Run() error {
	const (
		nodes          = 10
		timeoutCommit  = 15 * time.Second
		timeoutPropose = 10 * time.Second
		version        = dynamicTimeoutVersion
	)

	network, err := testnet.New("dynamic-timeouts", 864, nil, "test")
	if err != nil {
		return err
	}
	defer network.Cleanup()

	err = network.CreateGenesisNodes(nodes, version, 10000000, 0, testnet.DefaultResources)
	if err != nil {
		return err
	}

	gRPCEndpoints, err := network.RemoteGRPCEndpoints()
	if err != nil {
		return err
	}

	err = network.CreateTxClients(
		staticTimeoutVersion,
		1,
		"1000-128000",
		1,
		testnet.DefaultResources,
		gRPCEndpoints[:2],
	)
	if err != nil {
		return err
	}

	log.Printf("Setting up network\n")
	err = network.Setup(testnet.WithTimeoutCommit(timeoutCommit), testnet.WithTimeoutPropose(timeoutPropose))
	if err != nil {
		return err
	}

	log.Printf("Starting network\n")
	err = network.Start()
	if err != nil {
		return err
	}

	for _, node := range network.Nodes() {
		err = node.Instance.SetLatencyAndJitter(100, 10)
		if err != nil {
			return err
		}
	}

	// run the test for 5 minutes
	heightTicker := time.NewTicker(20 * time.Second)
	upgradeTicker := time.NewTicker(40 * time.Second)
	upgradeNodeIndex := 0
	loadTimeout := time.NewTimer(3 * time.Minute)
	timeout := time.NewTimer(6 * time.Minute)
	client, err := network.Node(0).Client()
	if err != nil {
		return err
	}
	for {
		select {
		case <-heightTicker.C:
			status, err := client.Status(context.Background())
			if err != nil {
				log.Printf("Error getting status: %v", err)
				continue
			}
			log.Printf("Height: %v", status.SyncInfo.LatestBlockHeight)

		case <-loadTimeout.C:
			network.StopTxClients()

		case <-upgradeTicker.C:
			continue
			n := network.Node(upgradeNodeIndex % nodes)
			n.Upgrade(dynamicTimeoutVersion)
			upgradeNodeIndex++

		case <-timeout.C:
			log.Println("--- COLLECTING DATA")
			if err := saveStartTimes(network); err != nil {
				log.Printf("Error saving start times: %v", err)
			}
			if err := saveBlockTimes(network); err != nil {
				log.Printf("Error saving block times: %v", err)
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
			return fmt.Errorf("error getting client for node %d: %v", i, err)
		}
		rpcClients[i] = client
		status, err := client.Status(context.Background())
		if err != nil {
			return fmt.Errorf("error getting status for node %d: %v", i, err)
		}
		if earliestLatestHeight == 0 || earliestLatestHeight < status.SyncInfo.LatestBlockHeight {
			earliestLatestHeight = status.SyncInfo.LatestBlockHeight
		}
	}

	// Create a CSV file
	file, err := os.Create(fmt.Sprintf("%s-%s-start-times.csv", time.Now().Format("2006-01-02-15-04-05"), testnet.Node(0).Version))
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

	firstTime := int64(0)

	// Write data for each height
	var errCount = 0
	for height := int64(1); height < earliestLatestHeight; height++ {
		row := make([]string, len(rpcClients))
		errCount = 0
		for i, client := range rpcClients {
			resp, err := client.StartTime(context.Background(), &height)
			if err != nil {
				log.Printf("Error getting start time for height %d and node %d: %v", height, i, err)
				errCount++
			} else {
				if firstTime == 0 {
					// subtract 10 seconds from the first time
					firstTime = resp.StartTime.UnixNano() - 1e10
				}
				row[i] = fmt.Sprintf("%d", resp.StartTime.UnixNano()-firstTime)
			}
		}
		if errCount == len(rpcClients) {
			return fmt.Errorf("all nodes failed to get start time for height %d", height)
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func saveBlockTimes(testnet *testnet.Testnet) error {
	file, err := os.Create(fmt.Sprintf("%s-%s-block-times.csv", time.Now().Format("2006-01-02-15-04-05"), testnet.Node(0).Version))
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	err = writer.Write([]string{"height", "block time", "block size", "last commit round"})
	if err != nil {
		return err
	}

	nodes := testnet.Nodes()
	clients := make([]*http.HTTP, len(nodes))
	for i, node := range nodes {
		clients[i], err = node.Client()
		if err != nil {
			return err
		}
	}
	status, err := clients[0].Status(context.Background())
	if err != nil {
		return err
	}
	index := 0
	for height := status.SyncInfo.EarliestBlockHeight; height <= status.SyncInfo.LatestBlockHeight; height++ {
		resp, err := clients[index].Block(context.Background(), &height)
		if err != nil {
			log.Printf("Error getting header for height %d: %v", height, err)
			index++
			if index == len(nodes) {
				return fmt.Errorf("all nodes failed to get header for height %d", height)
			}
			// retry the height
			height--
			continue
		}
		blockSize := 0
		for _, tx := range resp.Block.Txs {
			blockSize += len(tx)
		}
		err = writer.Write([]string{fmt.Sprintf("%d", height), fmt.Sprintf("%d", resp.Block.Time.UnixNano()), fmt.Sprintf("%d", blockSize), fmt.Sprintf("%d", resp.Block.LastCommit.Round)})
		if err != nil {
			return err
		}
	}
	return nil
}
