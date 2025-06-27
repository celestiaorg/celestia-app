package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cometbft/cometbft/types"
)

type BlockMetrics struct {
	blockTimes  []time.Time
	blockSizes  []int
	windowSize  int
	totalBlocks int64
}

func NewBlockMetrics(windowSize int) *BlockMetrics {
	return &BlockMetrics{
		blockTimes: make([]time.Time, 0, windowSize),
		blockSizes: make([]int, 0, windowSize),
		windowSize: windowSize,
	}
}

func (bm *BlockMetrics) AddBlock(timestamp time.Time, size int) {
	bm.blockTimes = append(bm.blockTimes, timestamp)
	bm.blockSizes = append(bm.blockSizes, size)
	bm.totalBlocks++

	// Keep only the last windowSize elements
	if len(bm.blockTimes) > bm.windowSize {
		bm.blockTimes = bm.blockTimes[1:]
		bm.blockSizes = bm.blockSizes[1:]
	}
}

func (bm *BlockMetrics) CalculateMetrics() (float64, float64, float64) {
	if len(bm.blockTimes) < 2 {
		return 0, 0, 0
	}

	// Calculate average block time
	totalDuration := bm.blockTimes[len(bm.blockTimes)-1].Sub(bm.blockTimes[0])
	avgBlockTime := totalDuration.Seconds() / float64(len(bm.blockTimes)-1)

	// Calculate total size and average throughput
	var totalSize int
	for _, size := range bm.blockSizes {
		totalSize += size
	}

	avgBlockSize := float64(totalSize) / float64(len(bm.blockSizes))

	// Calculate throughput in MB/s
	throughput := (float64(totalSize) / totalDuration.Seconds()) / (1024 * 1024)

	return avgBlockTime, avgBlockSize, throughput
}

func main() {
	if err := Run(); err != nil {
		fmt.Printf("ERROR: %s", err.Error())
	}
}

func Run() error {
	if len(os.Args) <= 1 {
		fmt.Printf("Usage: %s <node_rpc>\n", os.Args[0])
		return nil
	}

	url := os.Args[1]
	client, err := http.New(url, "/websocket")
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	if err := client.Start(); err != nil {
		return fmt.Errorf("failed to start client: %w", err)
	}
	defer func() {
		err := client.Stop()
		if err != nil {
			fmt.Printf("error while stopping node: %s", err.Error())
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewBlockMetrics(100) // Keep track of last 100 blocks

	newBlocksSub, err := client.Subscribe(ctx, "blocks-watcher", "tm.event = 'NewBlock'")
	if err != nil {
		return fmt.Errorf("failed to subscribe to new blocks: %w", err)
	}
	defer func() {
		err := client.Unsubscribe(ctx, "blocks-watcher", "tm.event = 'NewBlock'")
		if err != nil {
			fmt.Printf("error while unsubscribing: %s", err.Error())
		}
	}()

	fmt.Println("Listening for new blocks...")

	for {
		select {
		case event := <-newBlocksSub:
			blockEvent := event.Data.(types.EventDataNewBlock)
			block := blockEvent.Block

			// Calculate block size (including transactions)
			blockSize := 0
			for _, tx := range block.Txs {
				blockSize += len(tx)
			}

			metrics.AddBlock(block.Time, blockSize)

			// Calculate and print metrics immediately after adding a block
			avgBlockTime, avgBlockSize, throughput := metrics.CalculateMetrics()
			fmt.Printf("\n=== Network Metrics (last %d blocks) ===\n", metrics.windowSize)
			fmt.Printf("Total Blocks Processed: %d\n", metrics.totalBlocks)
			fmt.Printf("Average Block Time: %.2f seconds\n", avgBlockTime)
			fmt.Printf("Average Block Size: %.2f MB\n", avgBlockSize/(1024*1024))
			fmt.Printf("Network Throughput: %.2f MB/s\n", throughput)

		case <-ctx.Done():
			return nil
		}
	}
}
