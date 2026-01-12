package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/celestiaorg/celestia-app/v7/pkg/da"
	"github.com/celestiaorg/celestia-app/v7/pkg/wrapper"
	"github.com/celestiaorg/rsmt2d"
	"github.com/cometbft/cometbft/rpc/client/http"
	"github.com/spf13/cobra"
)

func main() {
	var rpc string

	rootCmd := &cobra.Command{
		Use:   "check-eds-construction",
		Short: "Check EDS construction consistency",
		Long: `Tool to verify that EDS construction produces consistent results
with and without tree pool optimization.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if rpc == "" {
				return fmt.Errorf("rpc is required")
			}
			return nil
		},
	}

	rootCmd.PersistentFlags().StringVar(&rpc, "rpc", "", "Node RPC endpoint (required)")
	_ = rootCmd.MarkPersistentFlagRequired("rpc")

	checkCmd := &cobra.Command{
		Use:   "check [height]",
		Short: "Check a specific block",
		Long:  `Check EDS construction for a specific block height`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			blockHeight, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid height: %w", err)
			}
			if blockHeight <= 0 {
				return fmt.Errorf("height must be positive")
			}

			treePool, err := wrapper.DefaultPreallocatedTreePool(512)
			if err != nil {
				return fmt.Errorf("failed to create tree pool: %w", err)
			}
			return checkBlock(rpc, blockHeight, treePool)
		},
	}

	randomCmd := &cobra.Command{
		Use:   "random [n]",
		Short: "Check random blocks",
		Long:  `Check EDS construction for n randomly selected blocks`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			numBlocks := 10
			if len(args) > 0 {
				var err error
				numBlocks, err = strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("invalid number of blocks: %w", err)
				}
				if numBlocks <= 0 {
					return fmt.Errorf("number of blocks must be positive")
				}
			}

			delay, _ := cmd.Flags().GetInt("delay")
			treePool, err := wrapper.DefaultPreallocatedTreePool(512)
			if err != nil {
				return fmt.Errorf("failed to create tree pool: %w", err)
			}
			return checkRandomBlocks(rpc, numBlocks, treePool, time.Duration(delay)*time.Millisecond)
		},
	}
	randomCmd.Flags().Int("delay", 100, "Delay between block checks in milliseconds")
	rootCmd.AddCommand(checkCmd, randomCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func checkBlock(url string, height int64, treePool *wrapper.TreePool) error {
	c, err := http.New(url, "/websocket")
	if err != nil {
		return fmt.Errorf("failed to create RPC client: %w", err)
	}

	resp, err := c.Status(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}
	fmt.Printf("Connected to %s on chain %s\n", url, resp.NodeInfo.Network)

	block, err := c.Block(context.Background(), &height)
	if err != nil {
		return fmt.Errorf("failed to get block at height %d: %w", height, err)
	}

	return compareEDSConstructions(block.Block.Txs.ToSliceOfBytes(), block.Block.Version.App, block.Block.DataHash, height, treePool)
}

func checkRandomBlocks(url string, numBlocks int, treePool *wrapper.TreePool, delay time.Duration) error {
	c, err := http.New(url, "/websocket")
	if err != nil {
		return fmt.Errorf("failed to create RPC client: %w", err)
	}

	status, err := c.Status(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	latestHeight := status.SyncInfo.LatestBlockHeight
	earliestHeight := status.SyncInfo.EarliestBlockHeight
	fmt.Printf("Connected to %s on chain %s\n", url, status.NodeInfo.Network)
	fmt.Printf("Latest block height: %d\n", latestHeight)

	if latestHeight-earliestHeight+1 < int64(numBlocks) {
		return fmt.Errorf("not enough blocks: latest height is %d but requested %d blocks", latestHeight, numBlocks)
	}

	selectedHeights := generateRandomHeights(earliestHeight, latestHeight, numBlocks)

	fmt.Printf("\nChecking %d random blocks with %dms delay between checks...\n", numBlocks, delay.Milliseconds())
	for i, height := range selectedHeights {
		fmt.Printf("\n[%d/%d] Checking block at height %d\n", i+1, numBlocks, height)

		block, err := c.Block(context.Background(), &height)
		if err != nil {
			return fmt.Errorf("failed to get block at height %d: %w", height, err)
		}
		err = compareEDSConstructions(block.Block.Txs.ToSliceOfBytes(), block.Block.Version.App, block.Block.DataHash, height, treePool)
		if err != nil {
			return fmt.Errorf("failed to compare EDS constructions for block at height %d: %w", height, err)
		}
		fmt.Printf("Block %d passed\n", height)

		if i < len(selectedHeights)-1 && delay > 0 {
			time.Sleep(delay)
		}
	}

	return nil
}

func generateRandomHeights(minHeight, maxHeight int64, count int) []int64 {
	source := rand.NewSource(time.Now().UnixNano())
	rng := rand.New(source)

	selected := make(map[int64]struct{})
	heights := make([]int64, 0, count)

	for len(heights) < count && len(selected) < int(maxHeight-minHeight+1) {
		// random between minHeight and maxHeight
		h := rng.Int63n(maxHeight-(minHeight-1)) + minHeight
		if _, exists := selected[h]; !exists {
			selected[h] = struct{}{}
			heights = append(heights, h)
		}
	}

	return heights
}

func compareEDSConstructions(txs [][]byte, appVersion uint64, blockDataHash []byte, height int64, treePool *wrapper.TreePool) error {
	eds, err := constructEDS(txs, appVersion)
	if err != nil {
		return fmt.Errorf("failed to construct EDS: %w", err)
	}

	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		return fmt.Errorf("failed to create DAH: %w", err)
	}

	edsWithPool, err := da.ConstructEDSWithTreePool(txs, appVersion, -1, treePool)
	if err != nil {
		return fmt.Errorf("failed to construct EDS with pool: %w", err)
	}

	dahWithPool, err := da.NewDataAvailabilityHeader(edsWithPool)
	if err != nil {
		return fmt.Errorf("failed to create DAH with pool: %w", err)
	}

	fmt.Printf("Got data root: %X\n", blockDataHash)
	fmt.Printf("Computed data root: %X\n", dah.Hash())
	fmt.Printf("Computed data root (with pool): %X\n", dahWithPool.Hash())

	dahHash := dah.Hash()
	dahWithPoolHash := dahWithPool.Hash()

	if string(dahHash) != string(dahWithPoolHash) {
		return fmt.Errorf("mismatch: EDS roots differ between pooled and not pooled")
	}
	if string(blockDataHash) != string(dahHash) {
		return fmt.Errorf("mismatch: computed root does not match block data hash")
	}

	fmt.Printf("All roots match!\n")
	return nil
}

func constructEDS(txs [][]byte, appVersion uint64) (*rsmt2d.ExtendedDataSquare, error) {
	return da.ConstructEDS(txs, appVersion, -1)
}
