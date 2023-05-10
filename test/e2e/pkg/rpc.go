package e2e

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
)

const (
	waitForHeightTimeout = 20 * time.Second
	maxTimePerBlock      = 20 * time.Second
)

// WaitForNBlocks queries the current latest height and waits until the network
// progresses to "blocks" blocks ahead.
func WaitForNBlocks(ctx context.Context, testnet *Testnet, blocks int64) error {
	height, err := GetHeights(ctx, testnet)
	if err != nil {
		return err
	}

	fmt.Printf("Waiting for the network to reach height %d\n", height[0]+blocks)

	return WaitForHeight(ctx, testnet, height[0]+blocks)
}

// WaitForHeight waits until the first node reaches the height specified. If
// no progress is made within a 20 second window then the function times out
// with an error.
func WaitForHeight(ctx context.Context, testnet *Testnet, height int64) error {
	var (
		err          error
		maxHeight    int64
		clients      = map[string]*rpchttp.HTTP{}
		lastIncrease = time.Now()
	)

	for {
		for _, node := range testnet.Nodes {
			if node.StartHeight > height {
				continue
			}
			client, ok := clients[node.Name]
			if !ok {
				client, err = node.Client()
				if err != nil {
					continue
				}
				clients[node.Name] = client
			}
			subctx, cancel := context.WithTimeout(ctx, 1*time.Second)
			defer cancel()

			// request the status of the node
			result, err := client.Status(subctx)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				continue
			}

			if result.SyncInfo.LatestBlockHeight >= height {
				return nil
			}

			if result.SyncInfo.LatestBlockHeight > maxHeight {
				maxHeight = result.SyncInfo.LatestBlockHeight
				lastIncrease = time.Now()
			}

			// If no progress has been made in the last 20 seconds, return an error.
			if time.Since(lastIncrease) > waitForHeightTimeout {
				return fmt.Errorf("network unable to reach next height within %s (max height: %d, target height: %d)",
					waitForHeightTimeout, maxHeight, height,
				)
			}

			time.Sleep(1 * time.Second)
		}
	}
}

// GetHeights loops through all running nodes and returns an array of heights
// in order of highest to lowest.
func GetHeights(ctx context.Context, testnet *Testnet) ([]int64, error) {
	var (
		err     error
		heights = make([]int64, 0, len(testnet.Nodes))
		clients = map[string]*rpchttp.HTTP{}
	)

	for _, node := range testnet.Nodes {
		client, ok := clients[node.Name]
		if !ok {
			client, err = node.Client()
			if err != nil {
				continue
			}
			clients[node.Name] = client
		}
		subctx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()

		// request the status of the node
		result, err := client.Status(subctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			continue
		}

		heights = append(heights, result.SyncInfo.LatestBlockHeight)
	}
	if len(heights) == 0 {
		return nil, errors.New("network is not running")
	}

	// return heights in descending order
	sort.Slice(heights, func(i, j int) bool {
		return heights[i] > heights[j]
	})

	return heights, nil
}
