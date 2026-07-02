package abci

import (
	"context"
	"errors"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// TestWatchEmbeddedAppUnexpectedExit verifies the fix for
// https://github.com/celestiaorg/celestia-app/issues/7405: when the embedded
// app exits unexpectedly, g.Wait must return a non-nil error instead of
// blocking until an OS quit signal. It uses the real getCtx so the signal
// listener goroutine is part of the group, which is exactly what used to keep
// g.Wait from returning.
func TestWatchEmbeddedAppUnexpectedExit(t *testing.T) {
	t.Run("failed exit surfaces the exit error", func(t *testing.T) {
		m := &Multiplexer{logger: log.NewNopLogger()}
		m.g, m.ctx = getCtx(server.NewDefaultContext())

		exited := make(chan error, 1)
		m.watchEmbeddedApp(9, exited)

		exitErr := errors.New("exit status 1")
		exited <- exitErr

		err := waitWithTimeout(t, m.g)
		require.ErrorIs(t, err, exitErr)
		require.ErrorContains(t, err, "embedded app for version 9 exited unexpectedly")
	})

	t.Run("clean but unexpected exit is still an error", func(t *testing.T) {
		m := &Multiplexer{logger: log.NewNopLogger()}
		m.g, m.ctx = getCtx(server.NewDefaultContext())

		exited := make(chan error, 1)
		m.watchEmbeddedApp(9, exited)

		exited <- nil

		err := waitWithTimeout(t, m.g)
		require.ErrorContains(t, err, "embedded app for version 9 exited unexpectedly")
	})
}

// TestWatchEmbeddedAppExpectedStop verifies that an exit which follows an
// operator-initiated stop (version switch or graceful shutdown) is not
// reported as an error. stopEmbeddedApp cancels the watcher before
// interrupting the child, so the watcher must return nil in that case.
func TestWatchEmbeddedAppExpectedStop(t *testing.T) {
	m := &Multiplexer{logger: log.NewNopLogger()}
	m.g, m.ctx = errgroup.WithContext(t.Context())

	exited := make(chan error, 1)
	m.watchEmbeddedApp(9, exited)

	// stopEmbeddedApp cancels the watcher before interrupting the child.
	m.cancelEmbeddedAppWatcher()
	exited <- errors.New("signal: interrupt")

	require.NoError(t, waitWithTimeout(t, m.g))
}

// TestWatchEmbeddedAppShutdown verifies that the watcher returns nil when the
// multiplexer's context is cancelled (e.g. an OS quit signal), so g.Wait can
// return and the deferred multiplexer.Stop can stop the child.
func TestWatchEmbeddedAppShutdown(t *testing.T) {
	m := &Multiplexer{logger: log.NewNopLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	m.g, m.ctx = errgroup.WithContext(ctx)

	exited := make(chan error, 1)
	m.watchEmbeddedApp(9, exited)

	cancel()

	require.NoError(t, waitWithTimeout(t, m.g))
}

// waitWithTimeout returns the result of g.Wait, failing the test if it does
// not return within a timeout.
func waitWithTimeout(t *testing.T, g *errgroup.Group) error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- g.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("g.Wait() did not return")
		return nil
	}
}
