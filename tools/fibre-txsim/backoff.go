package main

import (
	"context"
	"math/rand/v2"
	"time"
)

const maxSubmissionBackoff = 10 * time.Second

type submissionBackoff struct {
	ceiling time.Duration
}

func (b *submissionBackoff) next(succeeded bool) time.Duration {
	return b.nextWithJitter(succeeded, rand.Int64N)
}

func (b *submissionBackoff) nextWithJitter(succeeded bool, jitter func(int64) int64) time.Duration {
	if succeeded {
		b.ceiling = 0
		return 0
	}
	if b.ceiling == 0 {
		b.ceiling = 500 * time.Millisecond
	} else {
		b.ceiling = min(b.ceiling*2, maxSubmissionBackoff)
	}
	// Equal jitter keeps a nonzero pause while spreading retries across workers.
	half := b.ceiling / 2
	return half + time.Duration(jitter(int64(half)+1))
}

func waitSubmission(ctx context.Context, delay time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	if delay == 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
