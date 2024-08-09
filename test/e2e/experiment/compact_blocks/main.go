package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	// "github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/rpc/client/http"
)

const (
	compactBlocksVersion = "8f93462"
)

func main() {
	if err := Run(); err != nil {
		log.Fatalf("failed to run experiment: %v", err)
	}
}

func Run() error {
	const (
		nodes          = 5
		timeoutCommit  = time.Second
		timeoutPropose = 4 * time.Second
		version        = compactBlocksVersion
	)

	// blobParams := blobtypes.DefaultParams()
	// // set the square size to 128
	// blobParams.GovMaxSquareSize = 128
	// ecfg := encoding.MakeConfig(app.ModuleBasics)

	network, err := testnet.New("compact-blocks", 864, nil, "test")
	if err != nil {
		return err
	}
	defer network.Cleanup()

	err = network.CreateGenesisNodes(nodes, version, 10000000, 0, testnet.DefaultResources)
	if err != nil {
		return err
	}

	// cparams := app.DefaultConsensusParams()
	// cparams.Block.MaxBytes = 8 * 1024 * 1024
	// network.SetConsensusParams(cparams)

	gRPCEndpoints, err := network.RemoteGRPCEndpoints()
	if err != nil {
		return err
	}

	err = network.CreateTxClients(
		compactBlocksVersion,
		100,
		"4000-16000",
		1,
		testnet.DefaultResources,
		gRPCEndpoints[:3],
	)
	if err != nil {
		return err
	}

	log.Printf("Setting up network\n")
	err = network.Setup(
		testnet.WithTimeoutCommit(timeoutCommit),
		testnet.WithTimeoutPropose(timeoutPropose),
		testnet.WithMempool("v2"),
		func(cfg *config.Config) {
			// create a partially connected network by only dialing 5 peers
			cfg.P2P.MaxNumOutboundPeers = 4
			cfg.Mempool.TTLNumBlocks = 100
			cfg.Mempool.TTLDuration = 10 * time.Minute
			cfg.Mempool.MaxTxsBytes *= 4
		},
	)
	if err != nil {
		return err
	}

	log.Printf("Starting network\n")
	err = network.Start()
	if err != nil {
		return err
	}

	// run the test for 5 minutes
	heightTicker := time.NewTicker(20 * time.Second)
	timeout := time.NewTimer(5 * time.Minute)
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

		case <-timeout.C:
			if err := network.StopTxClients(); err != nil {
				log.Printf("Error stopping tx clients: %v", err)
			}
			log.Println("--- COLLECTING DATA")
			throughput, err := saveBlockTimes(network)
			if err != nil {
				log.Printf("Error saving block times: %v", err)
			}
			log.Printf("Throughput: %v", throughput)
			log.Println("--- FINISHED ✅: Compact Blocks")
			return nil
		}
	}
}

func saveBlockTimes(testnet *testnet.Testnet) (float64, error) {
	file, err := os.Create(fmt.Sprintf("%s-%s-block-times.csv", time.Now().Format("2006-01-02-15-04-05"), testnet.Node(0).Version))
	if err != nil {
		return 0, err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	err = writer.Write([]string{"height", "block time", "block size", "last commit round"})
	if err != nil {
		return 0, err
	}

	nodes := testnet.Nodes()
	clients := make([]*http.HTTP, len(nodes))
	for i, node := range nodes {
		clients[i], err = node.Client()
		if err != nil {
			return 0, err
		}
	}

	totalBlockSize := 0
	startTime := int64(0)
	status, err := clients[0].Status(context.Background())
	if err != nil {
		return 0, err
	}
	index := 0
	for height := status.SyncInfo.EarliestBlockHeight; height <= status.SyncInfo.LatestBlockHeight; height++ {
		resp, err := clients[index].Block(context.Background(), &height)
		if err != nil {
			log.Printf("Error getting header for height %d: %v", height, err)
			index++
			if index == len(nodes) {
				return 0, fmt.Errorf("all nodes failed to get header for height %d", height)
			}
			// retry the height
			height--
			continue
		}
		blockSize := 0
		for _, tx := range resp.Block.Txs {
			blockSize += len(tx)
		}
		if blockSize > 0 {
			totalBlockSize += blockSize
			if startTime == 0 {
				startTime = resp.Block.Time.UnixNano()
			}
		}
		if resp.Block.LastCommit.Round > 0 {
			log.Printf("Block %d has a last commit round of %d", resp.Block.LastCommit.Height, resp.Block.LastCommit.Round)
		}
		err = writer.Write([]string{fmt.Sprintf("%d", height), fmt.Sprintf("%d", resp.Block.Time.UnixNano()), fmt.Sprintf("%d", blockSize), fmt.Sprintf("%d", resp.Block.LastCommit.Round)})
		if err != nil {
			return 0, err
		}
	}

	duration := time.Since(time.Unix(0, startTime))
	return float64(totalBlockSize) / duration.Seconds(), nil
}
