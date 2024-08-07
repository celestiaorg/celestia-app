package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/tendermint/tendermint/rpc/client/http"
)

const (
	// blockTime is the observed average time between blocks. You can update this
	// value based on the block time on https://www.mintscan.io/celestia/block/ or
	// the output from the blocktime tool.
	blockTime = 11.30 // seconds between blocks for Arabica

	// exampleNodeRPC is an example node RPC endpoint for the Arabica testnet.
	exampleNodeRPC = "https://rpc.celestia-arabica-11.com:443"

	// targetTime is an example target time for the block height prediction.
	targetTime = "2024-08-14T14:00:00"

	// layout is the expected time format for targetTime.
	layout = "2006-01-02T15:04:05"
)

func main() {
	if err := Run(); err != nil {
		fmt.Printf("ERROR: %s", err.Error())
	}
}

func Run() error {
	if len(os.Args) < 3 {
		fmt.Printf("Usage: %s <node_rpc> <target_time>\n", os.Args[0])
		fmt.Printf("Example: %s %s %s\n", os.Args[0], exampleNodeRPC, targetTime)
		return nil
	}

	_, nodeRPC, targetTimeArg := os.Args[0], os.Args[1], os.Args[2]
	c, err := http.New(nodeRPC, "/websocket")
	if err != nil {
		return err
	}
	resp, err := c.Status(context.Background())
	if err != nil {
		return err
	}
	chainID := resp.NodeInfo.Network
	currentHeight := resp.SyncInfo.LatestBlockHeight
	currentTime := resp.SyncInfo.LatestBlockTime

	targetTime, err := time.Parse(layout, targetTimeArg)
	if err != nil {
		return fmt.Errorf("error parsing target time: %v", err)
	}

	if currentTime.After(targetTime) {
		return fmt.Errorf("current time %v is already after target time %v", currentTime, targetTime)
	}

	diffInSeconds := targetTime.Sub(currentTime).Seconds()
	diffInBlockHeight := math.Floor(diffInSeconds / blockTime)
	targetHeight := currentHeight + int64(diffInBlockHeight)

	fmt.Printf("chainID: %v\n", chainID)
	fmt.Printf("currentHeight: %v\n", currentHeight)
	fmt.Printf("currentTime: %v\n", currentTime.String())
	fmt.Printf("targetHeight: %v\n", targetHeight)
	fmt.Printf("targetTime: %v\n", targetTime.String())
	fmt.Printf("diffInSeconds: %v\n", math.Floor(diffInSeconds))
	fmt.Printf("diffInBlockHeight: %v\n", diffInBlockHeight)
	return nil
}
