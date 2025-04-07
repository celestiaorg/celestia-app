package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cometbft/cometbft/rpc/client/http"
)

// getHeight retrieves the current block height from the provided HTTP client within the specified timeout duration.
// It repeatedly checks the client's status every 100ms until the timeout expires or the height is successfully retrieved.
// Returns the latest block height or an error if the operation times out or the context is canceled.
func getHeight(ctx context.Context, client *http.HTTP, period time.Duration) (int64, error) {
	timer := time.NewTimer(period)
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-timer.C:
			return 0, fmt.Errorf("failed to get height after %.2f seconds", period.Seconds())
		case <-ticker.C:
			status, err := client.Status(ctx)
			if err == nil {
				return status.SyncInfo.LatestBlockHeight, nil
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return 0, err
			}
		}
	}
}

// waitForHeight waits until the blockchain reaches the specified height or the timeout period expires.
// It polls the current block height using the provided HTTP client at regular intervals of 100ms.
// Returns nil if the target height is reached, or an error if the timeout is exceeded or the context is canceled.
func waitForHeight(ctx context.Context, client *http.HTTP, height int64, period time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, period)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for height %d", height)
		case <-ticker.C:
			currentHeight, err := getHeight(ctx, client, period)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				continue
			}
			if currentHeight >= height {
				return nil
			}
		}
	}
}
