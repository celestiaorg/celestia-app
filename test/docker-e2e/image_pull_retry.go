package docker_e2e

import (
	"context"
	"fmt"
	"time"

	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
)

// retryPull invokes op up to maxAttempts times, sleeping baseDelay*2^attempt
// between attempts, and aborts early if ctx is cancelled. Returns the final
// op error on exhaustion.
func retryPull(ctx context.Context, maxAttempts int, baseDelay time.Duration, op func() error) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("retryPull: %w", err)
		}
		lastErr = op()
		if lastErr == nil {
			return nil
		}
		if attempt == maxAttempts-1 {
			break
		}
		delay := baseDelay << attempt
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return fmt.Errorf("retryPull: %w", ctx.Err())
		}
	}
	return fmt.Errorf("retryPull exhausted after %d attempts: %w", maxAttempts, lastErr)
}

// pullImageWithRetry pulls the given image via tastora's idempotent PullImage,
// retrying on transient registry errors (e.g. ghcr.io timeouts). Safe to call
// on images that are already cached locally — PullImage is a no-op in that case.
func pullImageWithRetry(ctx context.Context, client tastoratypes.TastoraDockerClient, image tastoracontainertypes.Image) error {
	return retryPull(ctx, 3, 2*time.Second, func() error {
		return image.PullImage(ctx, client)
	})
}
