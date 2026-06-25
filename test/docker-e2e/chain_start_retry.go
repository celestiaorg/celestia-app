package docker_e2e

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// isPortBindingError reports whether err indicates a Docker host-port
// allocation collision. tastora allocates an ephemeral host port, closes the
// probe listener, then asks Docker to publish that port; if a prior chain's
// container has not yet released the same port (or another process grabbed it
// in the interim) Docker fails with "address already in use". Rebuilding the
// chain allocates fresh ports, so these errors are safe to retry.
func isPortBindingError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "address already in use")
}

// retryOnPortCollision invokes op up to maxAttempts times, retrying only while
// op fails with a port-binding collision (see isPortBindingError) and sleeping
// baseDelay*2^attempt between attempts. Any other error is returned
// immediately, and ctx cancellation aborts early.
func retryOnPortCollision(ctx context.Context, maxAttempts int, baseDelay time.Duration, op func() error) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("retryOnPortCollision: %w", err)
		}
		lastErr = op()
		if lastErr == nil {
			return nil
		}
		if !isPortBindingError(lastErr) {
			return lastErr
		}
		if attempt == maxAttempts-1 {
			break
		}
		delay := baseDelay << attempt
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return fmt.Errorf("retryOnPortCollision: %w", ctx.Err())
		}
	}
	return fmt.Errorf("retryOnPortCollision exhausted after %d attempts: %w", maxAttempts, lastErr)
}
