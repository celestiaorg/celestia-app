package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestUploadLimiter(t *testing.T) {
	require.Equal(t, rate.Inf, newUploadLimiter(0).Limit())

	const target = 50.0
	limiter := newUploadLimiter(target)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Many workers share one bucket, so the total stays at the target.
	var uploads atomic.Int64
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for limiter.Wait(ctx) == nil {
				uploads.Add(1)
			}
		})
	}
	wg.Wait()
	require.InDelta(t, target, float64(uploads.Load()), target*0.1)
}

func TestRunRejectsNegativeTargetRate(t *testing.T) {
	err := run(config{concurrency: 1, targetRate: -1})
	require.ErrorContains(t, err, "--target-rate")
}
