package internal

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitFor(t *testing.T) {
	t.Run("retries until ready", func(t *testing.T) {
		calls := 0
		err := WaitFor(context.Background(), time.Second, time.Millisecond, func(context.Context) (bool, error) {
			calls++
			return calls == 3, nil
		})
		if err != nil || calls != 3 {
			t.Fatalf("got %d calls and error %v", calls, err)
		}
	})
	t.Run("bounds blocked query", func(t *testing.T) {
		err := WaitFor(context.Background(), 10*time.Millisecond, time.Millisecond, func(ctx context.Context) (bool, error) {
			<-ctx.Done()
			return false, ctx.Err()
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded, got %v", err)
		}
	})
	t.Run("canceled parent skips query", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := WaitFor(ctx, time.Second, time.Millisecond, func(context.Context) (bool, error) {
			t.Fatal("query called after cancellation")
			return false, nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	})
}
