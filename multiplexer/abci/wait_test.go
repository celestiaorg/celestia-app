package abci

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// TestWaitForEmbeddedApp verifies that the multiplexer returns when the embedded
// app exits unexpectedly, rather than blocking forever on the signal handler.
func TestWaitForEmbeddedApp(t *testing.T) {
	t.Run("returns the fatal error when an embedded app exits unexpectedly", func(t *testing.T) {
		// g mimics the real errgroup whose signal-handler goroutine blocks until
		// an OS signal that never arrives in this test.
		g := &errgroup.Group{}
		g.Go(func() error {
			select {} // block forever
		})

		m := &Multiplexer{g: g, fatalCh: make(chan struct{})}

		fatal := errors.New("embedded app for version 5 exited unexpectedly")
		m.fatalErr = fatal
		close(m.fatalCh)

		done := make(chan error, 1)
		go func() { done <- m.waitForEmbeddedApp() }()

		select {
		case err := <-done:
			require.ErrorIs(t, err, fatal)
		case <-time.After(5 * time.Second):
			t.Fatal("waitForEmbeddedApp did not return after a fatal embedded app exit")
		}
	})

	t.Run("returns the errgroup result on graceful shutdown", func(t *testing.T) {
		g := &errgroup.Group{}
		// Simulate the signal handler returning nil after a quit signal.
		g.Go(func() error { return nil })

		m := &Multiplexer{g: g, fatalCh: make(chan struct{})}

		done := make(chan error, 1)
		go func() { done <- m.waitForEmbeddedApp() }()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("waitForEmbeddedApp did not return on graceful shutdown")
		}
	})
}
