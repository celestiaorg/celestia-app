package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/tendermint/tendermint/rpc/client/http"
)

const (
	// blockTime is the observed average time between blocks. You can update this
	// value based on the block time on https://www.mintscan.io/celestia/block/ or
	// the output from the blocktime tool.
	blockTime = 11.75 // seconds between blocks on Mainnet Beta.

	// exampleArabicaRPC is an example node RPC endpoint for the Arabica testnet.
	exampleArabicaRPC = "https://rpc.celestia-arabica-11.com:443"

	// exampleMochaRPC is an example node RPC endpoint for the Mocha testnet.
	exampleMochaRPC = "https://celestia-mocha-rpc.publicnode.com:443"

	// exampleMainnetHeight is an example node RPC endpoint for Mainnet Beta.
	exampleMainnetRPC = "https://celestia-rpc.publicnode.com:443"

	// exampleArabicaHeight is an example block height for the Arabica testnet.
	exampleArabicaHeight = 1751707

	// exampleMochaHeight is an example block height for the Mocha testnet.
	exampleMochaHeight = 2585031

	// exampleMainnetHeight is an example block height for Mainnet Beta.
	exampleMainnetHeight = 2371495
)

func main() {
	if err := Run(); err != nil {
		fmt.Printf("ERROR: %s", err.Error())
	}
}

func Run() error {
	if len(os.Args) < 3 {
		fmt.Printf("Usage: %s <node_rpc> <target_block_height>\n", os.Args[0])
		fmt.Printf("Example: %s %s %v\n", os.Args[0], exampleArabicaRPC, exampleArabicaHeight)
		fmt.Printf("Example: %s %s %v\n", os.Args[0], exampleMochaRPC, exampleMochaHeight)
		fmt.Printf("Example: %s %s %v\n", os.Args[0], exampleMainnetRPC, exampleMainnetHeight)
		return nil
	}

	_, nodeRPC, targetBlockHeightArg := os.Args[0], os.Args[1], os.Args[2]
	targetBlockHeight, err := strconv.ParseInt(targetBlockHeightArg, 10, 64)
	if err != nil {
		return err
	}
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

	if currentHeight >= targetBlockHeight {
		return fmt.Errorf("current height %v is already after target height %v", currentHeight, targetBlockHeight)
	}
	diffInBlockHeight := targetBlockHeight - currentHeight
	diffInSeconds := blockTime * float64(diffInBlockHeight)
	diffInTime, err := time.ParseDuration(fmt.Sprintf("%.0fs", diffInSeconds))
	if err != nil {
		return err
	}
	arrivalTime := currentTime.Add(diffInTime)

	fmt.Printf("chainID: %v\n", chainID)
	fmt.Printf("currentHeight: %v\n", currentHeight)
	fmt.Printf("currentTime: %v\n", currentTime.String())
	fmt.Printf("diffInBlockHeight: %v\n", diffInBlockHeight)
	fmt.Printf("diffInTime: %v\n", diffInTime)
	fmt.Printf("arrivalTime: %v\n", arrivalTime)
	return nil
}
