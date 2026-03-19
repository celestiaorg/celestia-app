package fibre

import (
	"context"
	"time"
)

const pruneInterval = time.Minute

// startPruneLoop starts a background goroutine that periodically prunes expired entries from the store.
// It runs every minute and removes entries with pruneAt times before the current time.
// The loop stops when the context is cancelled.
func (s *Server) startPruneLoop(ctx context.Context) {
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.prune(ctx)
		}
	}
}

func (s *Server) prune(ctx context.Context) {
	start := time.Now()

	pruned, err := s.store.PruneBefore(ctx, start)
	s.metrics.observePrune(ctx, start, pruned, err)
	if err != nil {
		s.log.ErrorContext(ctx, "failed to prune store", "error", err, "elapsed (ms)", time.Since(start).Milliseconds())
		return
	}

	if pruned > 0 {
		s.log.InfoContext(ctx, "pruned expired entries", "pruned", pruned, "elapsed (ms)", time.Since(start).Milliseconds())
	}
}
