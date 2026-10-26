package internal

import (
	"context"
	"fmt"
	"time"
)

// WaitFor polls cond immediately and at interval until it succeeds or times out.
// Conditions must use the supplied context so RPCs share the timeout.
func WaitFor(ctx context.Context, timeout, interval time.Duration, cond func(context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("condition not met within %s: %w", timeout, err)
		}
		ok, err := cond(ctx)
		if err != nil {
			return fmt.Errorf("checking condition: %w", err)
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("condition not met within %s: %w", timeout, ctx.Err())
		case <-ticker.C:
		}
	}
}
