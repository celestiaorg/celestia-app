package fibre

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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
	var pruneErr error
	defer func() {
		elapsed := time.Since(start)
		s.metrics.pruneDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(attribute.Bool("success", pruneErr == nil)))
	}()

	pruned, err := s.store.PruneBefore(ctx, start)
	pruneErr = err
	if err != nil {
		s.log.ErrorContext(ctx, "failed to prune store", "error", err, "elapsed (ms)", time.Since(start).Milliseconds())
		return
	}

	if pruned > 0 {
		s.metrics.pruneEntries.Add(ctx, int64(pruned))
		s.log.InfoContext(ctx, "pruned expired entries", "pruned", pruned, "elapsed (ms)", time.Since(start).Milliseconds())
	}
}
