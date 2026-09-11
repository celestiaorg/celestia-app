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
		deleteErr    error
		cursor       []byte
	)

	for {
		pruned, freed, next, err := s.store.pruneBefore(ctx, start, cursor)
		totalPruned += pruned
		if freed > 0 {
			s.occ.release(freed)
		}
		if err != nil {
			// Only a direct partial error permits progress; joined fatal errors must stop the pass.
			if _, partial := err.(*partialDeleteError); partial {
				if deleteErr == nil {
					deleteErr = err
				}
			} else if errors.Is(err, ErrStoreIntegrity) {
				if integrityErr == nil {
					integrityErr = err
				}
			} else {
				s.metrics.observePrune(ctx, start, totalPruned, err)
				s.log.ErrorContext(ctx, "failed to prune store", "error", err, "elapsed (ms)", time.Since(start).Milliseconds())
				return
			}
		}

		if len(next) == 0 || ctx.Err() != nil {
			break
		}
		cursor = next
	}

	if integrityErr != nil {
		s.log.WarnContext(ctx, "prune skipped corrupt shard markers", "error", integrityErr,
			"elapsed (ms)", time.Since(start).Milliseconds())
	}
	if deleteErr != nil {
		s.log.WarnContext(ctx, "prune retained failed payload deletions", "error", deleteErr,
			"elapsed (ms)", time.Since(start).Milliseconds())
	}
	s.metrics.observePrune(ctx, start, totalPruned, deleteErr)

	if totalPruned > 0 {
		s.log.InfoContext(ctx, "pruned expired entries", "pruned", totalPruned, "elapsed (ms)", time.Since(start).Milliseconds())
	}
}
