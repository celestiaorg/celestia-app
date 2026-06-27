package abci

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// fakeProcessStatus is a test double for the subset of *appd.Appd behavior the
// embedded-app monitor relies on.
type fakeProcessStatus struct {
	stopped       bool
	stopInitiated bool
	exitErr       error
}

func (f fakeProcessStatus) IsStopped() bool     { return f.stopped }
func (f fakeProcessStatus) StopInitiated() bool { return f.stopInitiated }
func (f fakeProcessStatus) ExitError() error    { return f.exitErr }

func TestEmbeddedAppExitError(t *testing.T) {
	t.Run("running app returns nil", func(t *testing.T) {
		err := embeddedAppExitError(5, fakeProcessStatus{stopped: false})
		require.NoError(t, err)
	})

	t.Run("operator-initiated stop returns nil", func(t *testing.T) {
		err := embeddedAppExitError(5, fakeProcessStatus{stopped: true, stopInitiated: true})
		require.NoError(t, err)
	})

	t.Run("unexpected exit returns error wrapping the exit error", func(t *testing.T) {
		exitErr := errors.New("boom")
		err := embeddedAppExitError(5, fakeProcessStatus{stopped: true, stopInitiated: false, exitErr: exitErr})
		require.Error(t, err)
		require.ErrorIs(t, err, exitErr)
	})
}

// TestWaitForEmbeddedAppReturnsOnErrgroupCompletion verifies that
// waitForEmbeddedApp returns the errgroup's result on a graceful shutdown
// (e.g. when a quit signal causes the errgroup goroutine to return) rather
// than blocking forever.
func TestWaitForEmbeddedAppReturnsOnErrgroupCompletion(t *testing.T) {
	m := &Multiplexer{logger: log.NewNopLogger()}
	g, ctx := errgroup.WithContext(context.Background())
	m.g, m.ctx = g, ctx

	sentinel := errors.New("graceful shutdown")
	m.g.Go(func() error { return sentinel })

	require.ErrorIs(t, m.waitForEmbeddedApp(), sentinel)
}
