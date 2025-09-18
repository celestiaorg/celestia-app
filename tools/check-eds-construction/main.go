package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"

	"github.com/celestiaorg/celestia-app/v6/pkg/da"
	"github.com/cometbft/cometbft/rpc/client/http"
)

func main() {
	if err := Run(); err != nil {
		fmt.Printf("ERROR: %s", err.Error())
	}
}

func Run() error {
	if len(os.Args) <= 2 {
		fmt.Printf("Usage: %s <node_rpc> <block_height>\n", os.Args[0])
		return nil
	}

	url := os.Args[1]
	c, err := http.New(url, "/websocket")
	if err != nil {
		return fmt.Errorf("failed to create RPC client: %w", err)
	}
	resp, err := c.Status(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}
	fmt.Printf("Connected to %s on chain %s\n", url, resp.NodeInfo.Network)
	height, err := strconv.ParseInt(os.Args[2], 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse block height: %w", err)
	}

	block, err := c.Block(context.Background(), &height)
	if err != nil {
		return fmt.Errorf("failed to get block: %w", err)
	}

	eds, err := da.ConstructEDS(block.Block.Txs.ToSliceOfBytes(), block.Block.Header.Version.App, -1)
	if err != nil {
		return fmt.Errorf("failed to construct EDS: %w", err)
	}

	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		return fmt.Errorf("failed to create DAH: %w", err)
	}

	fmt.Printf("Got data root: %X\n", block.Block.Header.DataHash)
	fmt.Printf("Computed data root: %X\n", dah.Hash())

	return nil
}
