package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestUploadLimiter(t *testing.T) {
	require.Equal(t, rate.Inf, newUploadLimiter(0).Limit())
	limiter := newUploadLimiter(2.5)
	require.Equal(t, rate.Limit(2.5), limiter.Limit())
	require.Equal(t, 1, limiter.Burst())
}

func TestRunRejectsNegativeTargetRate(t *testing.T) {
	err := run(config{concurrency: 1, targetRate: -1})
	require.ErrorContains(t, err, "--target-rate")
}
