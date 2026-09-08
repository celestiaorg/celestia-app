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
	var (
		totalPruned  int
		integrityErr error
	)

	for {
		pruned, freed, err := s.store.PruneBefore(ctx, start)
		if err != nil {
			if !errors.Is(err, ErrStoreIntegrity) {
				s.metrics.observePrune(ctx, start, totalPruned, err)
				s.log.ErrorContext(ctx, "failed to prune store", "error", err, "elapsed (ms)", time.Since(start).Milliseconds())
				return
			}
			if integrityErr == nil {
				integrityErr = err
			}
		}

		totalPruned += pruned
		if freed > 0 {
			s.occ.release(freed)
		}
		if pruned < maxPruneBatchSize || ctx.Err() != nil {
			break
		}
	}

	if integrityErr != nil {
		s.log.WarnContext(ctx, "prune skipped corrupt shard markers", "error", integrityErr,
			"elapsed (ms)", time.Since(start).Milliseconds())
	}
	s.metrics.observePrune(ctx, start, totalPruned, nil)

	if totalPruned > 0 {
		s.log.InfoContext(ctx, "pruned expired entries", "pruned", totalPruned, "elapsed (ms)", time.Since(start).Milliseconds())
	}
}
