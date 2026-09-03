package fibre

import (
	"context"
	"errors"
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
			// A transient failure here is non-fatal: the previously derived
			// budget (established at startup) is retained rather than dropping
			// to unlimited.
			if err := s.recomputeBudget(ctx); err != nil {
				s.log.WarnContext(ctx, "budget recompute failed; keeping previous budget", "error", err)
			}
		}
	}
}

func (s *Server) prune(ctx context.Context) {
	start := time.Now()
	var totalPruned int
	var pruneErr error

	for {
		pruned, freed, err := s.store.PruneBefore(ctx, start)
		totalPruned += pruned
		if freed > 0 {
			s.occ.release(freed)
		}
		if err != nil && (pruneErr == nil || !errors.Is(err, ErrStoreIntegrity)) {
			pruneErr = err
		}
		if pruned < maxPruneBatchSize || (err != nil && !errors.Is(err, ErrStoreIntegrity)) || ctx.Err() != nil {
			break
		}
	}
	metricErr := pruneErr
	if errors.Is(pruneErr, ErrStoreIntegrity) {
		s.log.WarnContext(ctx, "prune skipped corrupt shard markers", "error", pruneErr,
			"elapsed (ms)", time.Since(start).Milliseconds())
		metricErr = nil
	} else if pruneErr != nil {
		s.log.ErrorContext(ctx, "failed to prune store", "error", pruneErr, "elapsed (ms)", time.Since(start).Milliseconds())
	}
	s.metrics.observePrune(ctx, start, totalPruned, metricErr)

	if totalPruned > 0 {
		s.log.InfoContext(ctx, "pruned expired entries", "pruned", totalPruned, "elapsed (ms)", time.Since(start).Milliseconds())
	}
}
