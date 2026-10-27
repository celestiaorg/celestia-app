package docker_e2e

import (
	"context"
	"fmt"
	"time"
)

// startWithArchivePeerCheck cancels a stalled startup if no configured archive
// peer connects within peerTimeout of the first successful RPC status query.
// probe returns whether RPC is ready and whether an archive peer connected (or
// block syncing already made progress). Start retains its own block timeout.
func startWithArchivePeerCheck(ctx context.Context, start func(context.Context) error, probe func(context.Context) (bool, bool), peerTimeout, pollInterval time.Duration) error {
	ctx, cancel := context.WithCancel(ctx)
	started := make(chan struct{})
	var startErr error
	go func() {
		startErr = start(ctx)
		close(started)
	}()
	// Always join Start before returning so diagnostics and container cleanup
	// cannot race with a startup that is still using the node.
	defer func() {
		cancel()
		<-started
	}()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var deadline time.Time
	for {
		select {
		case <-started:
			return startErr
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ready, connected := probe(ctx)
			if connected {
				// Having connected to an archive peer, let the usual startup
				// timeout diagnose slow block processing or stale archive peers.
				<-started
				return startErr
			}
			if ready && deadline.IsZero() {
				deadline = time.Now().Add(peerTimeout)
			}
			if !deadline.IsZero() && !time.Now().Before(deadline) {
				return fmt.Errorf("no archive peer connected within %s after RPC became ready; check archive peer availability and P2P connectivity", peerTimeout)
			}
		}
	}
}
