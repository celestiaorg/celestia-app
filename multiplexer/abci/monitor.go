package abci

import (
	"fmt"
	"time"
)

// embeddedAppMonitorInterval is how often the multiplexer polls a running
// embedded app to detect an unexpected exit.
const embeddedAppMonitorInterval = time.Second

// processStatus is the subset of *appd.Appd behavior that the embedded-app
// monitor relies on. It exists to keep the monitor's decision logic testable.
type processStatus interface {
	// IsStopped reports whether the process has exited (or was never started).
	IsStopped() bool
	// StopInitiated reports whether Stop was called, i.e. the exit was
	// operator-initiated rather than an unexpected crash.
	StopInitiated() bool
	// ExitError returns the error returned by the process's exit, if any.
	ExitError() error
}

// embeddedAppExitError returns an error if the embedded app exited
// unexpectedly. It returns nil while the app is running and nil when the exit
// was operator-initiated (e.g. a version switch or graceful shutdown).
func embeddedAppExitError(appVersion uint64, p processStatus) error {
	if p == nil || !p.IsStopped() || p.StopInitiated() {
		return nil
	}
	return fmt.Errorf("embedded app for version %d exited unexpectedly: %w", appVersion, p.ExitError())
}

// waitForEmbeddedApp blocks until either the errgroup completes (e.g. a quit
// signal triggers a graceful shutdown) or the running embedded app exits
// unexpectedly. In the latter case it returns a non-nil error so the parent
// process exits non-zero instead of hanging silently.
//
// The errgroup cannot be relied upon to surface an unexpected child exit on its
// own: server.ListenForQuitSignals adds a goroutine that blocks until an OS
// signal is received and never observes the context, so g.Wait would not return
// when the child dies. waitForEmbeddedApp therefore polls the child alongside
// g.Wait and returns as soon as either fires.
func (m *Multiplexer) waitForEmbeddedApp() error {
	waitErrCh := make(chan error, 1)
	go func() { waitErrCh <- m.g.Wait() }()

	ticker := time.NewTicker(embeddedAppMonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case err := <-waitErrCh:
			return err
		case <-ticker.C:
			if err := m.checkEmbeddedAppRunning(); err != nil {
				m.logger.Error("embedded app exited unexpectedly", "err", err)
				return err
			}
		}
	}
}

// checkEmbeddedAppRunning returns an error if the active embedded app has
// exited unexpectedly. It returns nil when no embedded app is active.
func (m *Multiplexer) checkEmbeddedAppRunning() error {
	m.mu.Lock()
	version := m.activeVersion
	started := m.started
	m.mu.Unlock()

	if !started || version.Appd == nil {
		return nil
	}
	return embeddedAppExitError(version.AppVersion, version.Appd)
}
