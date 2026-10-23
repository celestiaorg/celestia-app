package docker_e2e

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStartWithArchivePeerCheck(t *testing.T) {
	t.Run("no archive peer cancels and joins startup", func(t *testing.T) {
		stopped := make(chan struct{})
		start := func(ctx context.Context) error {
			defer close(stopped)
			<-ctx.Done()
			return ctx.Err()
		}
		// A responsive node with only pruned peers must still fail fast.
		probe := func(context.Context) (bool, bool) { return true, false }
		err := startWithArchivePeerCheck(t.Context(), start, probe, 10*time.Millisecond, time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "no archive peer connected") {
			t.Fatalf("expected archive connectivity error, got %v", err)
		}
		select {
		case <-stopped:
		default:
			t.Fatal("startup was not joined before returning")
		}
	})

	t.Run("genesis initialization does not consume peer timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		defer cancel()
		start := func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}
		probe := func(context.Context) (bool, bool) { return false, false }
		err := startWithArchivePeerCheck(ctx, start, probe, time.Millisecond, time.Millisecond)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("peer timeout fired before RPC was ready: %v", err)
		}
	})

	t.Run("RPC failure after readiness does not reset deadline", func(t *testing.T) {
		start := func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}
		calls := 0
		probe := func(context.Context) (bool, bool) {
			calls++
			return calls == 1, false
		}
		err := startWithArchivePeerCheck(t.Context(), start, probe, 10*time.Millisecond, time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "no archive peer connected") {
			t.Fatalf("expected archive connectivity error, got %v", err)
		}
	})

	t.Run("archive connection or sync progress preserves startup result", func(t *testing.T) {
		for _, startErr := range []error{nil, errors.New("block processing failed")} {
			connected := make(chan struct{})
			start := func(context.Context) error {
				<-connected
				return startErr
			}
			probe := func(context.Context) (bool, bool) {
				close(connected)
				return true, true
			}
			err := startWithArchivePeerCheck(t.Context(), start, probe, time.Millisecond, time.Millisecond)
			if !errors.Is(err, startErr) {
				t.Fatalf("expected startup result %v, got %v", startErr, err)
			}
		}
	})

	t.Run("startup error is returned without waiting for RPC", func(t *testing.T) {
		startErr := errors.New("container failed")
		start := func(context.Context) error { return startErr }
		probe := func(context.Context) (bool, bool) { return false, false }
		err := startWithArchivePeerCheck(t.Context(), start, probe, time.Second, time.Millisecond)
		if !errors.Is(err, startErr) {
			t.Fatalf("expected startup error, got %v", err)
		}
	})
}
