package fibre

import (
	"context"
	"testing"
	"time"

	clock "github.com/filecoin-project/go-clock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newTestLimiter builds an uploadLimiter wired to a mock clock for
// deterministic rate accounting. rate/burst are in bytes, maxWait is the byte
// budget wait, inFlight is the in-flight cap.
func newTestLimiter(t *testing.T, rate, burst int, maxWait time.Duration, inFlight int) (*uploadLimiter, *clock.Mock) {
	t.Helper()
	mock := clock.NewMock()
	metrics, err := newServerMetrics(otel.Meter("fibre-test"))
	require.NoError(t, err)
	cfg := ServerConfig{
		UploadRateLimitEnabled:        true,
		UploadRateLimitBytesPerSecond: rate,
		UploadRateLimitBurstBytes:     burst,
		UploadRateLimitMaxWait:        maxWait.String(),
		MaxUploadShardInFlight:        inFlight,
		Clock:                         mock,
	}
	return newUploadLimiter(cfg, metrics), mock
}

func TestUploadLimiterDisabled(t *testing.T) {
	// rate <= 0 disables the whole controller (even with the toggle on).
	l, _ := newTestLimiter(t, 0, 0, 0, 0)
	require.False(t, l.enabled)

	// acquireSlot returns a usable no-op release regardless of count.
	for range 5 {
		release := l.acquireSlot()
		require.NotNil(t, release)
		release()
	}
	// acquireBytes admits any size, even absurdly large, immediately with no wait.
	wait, err := l.acquireBytes(t.Context(), 1<<30)
	require.NoError(t, err)
	require.Zero(t, wait)
	// observeRejected is a no-op (must not panic on nil metrics).
	l.observeRejected(t.Context(), "byte_budget")
}

func TestUploadLimiterDisabledByToggle(t *testing.T) {
	// Toggle off disables the controller even with a positive rate.
	mock := clock.NewMock()
	metrics, err := newServerMetrics(otel.Meter("fibre-test"))
	require.NoError(t, err)
	cfg := ServerConfig{
		UploadRateLimitEnabled:        false,
		UploadRateLimitBytesPerSecond: 1000,
		UploadRateLimitBurstBytes:     1000,
		UploadRateLimitMaxWait:        "0s",
		MaxUploadShardInFlight:        4,
		Clock:                         mock,
	}
	l := newUploadLimiter(cfg, metrics)
	require.False(t, l.enabled)
	wait, err := l.acquireBytes(t.Context(), 1<<30)
	require.NoError(t, err)
	require.Zero(t, wait)
}

func TestUploadLimiterFullBucketAdmitsImmediately(t *testing.T) {
	l, _ := newTestLimiter(t, 1000, 1000, 100*time.Millisecond, 4)
	// A full bucket admits up to burst with zero delay.
	wait, err := l.acquireBytes(t.Context(), 1000)
	require.NoError(t, err)
	require.Zero(t, wait)
}

func TestUploadLimiterSustainedRejectThenRefillAdmits(t *testing.T) {
	const rate, burst = 1000, 1000 // bytes/s, bytes
	l, mock := newTestLimiter(t, rate, burst, 100*time.Millisecond, 4)
	ctx := t.Context()

	// Drain the full bucket.
	_, err := l.acquireBytes(ctx, burst)
	require.NoError(t, err)

	// A 500-byte request now needs 500ms of refill, which exceeds the 100ms
	// maxWait, so it is rejected immediately (no clock advance).
	_, err = l.acquireBytes(ctx, 500)
	require.Error(t, err)
	require.Equal(t, grpccodes.ResourceExhausted, status.Code(err))

	// The rejected reservation refunds its tokens, so it did not drain the
	// bucket further. After 500ms of refill, the same request is admitted.
	mock.Add(500 * time.Millisecond)
	_, err = l.acquireBytes(ctx, 500)
	require.NoError(t, err)
}

func TestUploadLimiterMaxBlobAdmittedAfterNecessaryWait(t *testing.T) {
	// maxWait == burst/rate: a single max-size blob from a drained bucket is
	// admitted after exactly the necessary refill, not rejected.
	const rate, burst = 1000, 1000
	maxWait := time.Duration(burst) * time.Second / time.Duration(rate) // == 1s
	l, mock := newTestLimiter(t, rate, burst, maxWait, 4)
	ctx := t.Context()

	_, err := l.acquireBytes(ctx, burst) // drain
	require.NoError(t, err)
	// Full burst needs burst/rate == 1s of refill, which equals maxWait.
	mock.Add(maxWait)
	_, err = l.acquireBytes(ctx, burst)
	require.NoError(t, err)
}

func TestUploadLimiterRejectsSizeOverBurst(t *testing.T) {
	l, _ := newTestLimiter(t, 1000, 1000, 100*time.Millisecond, 4)
	// A request larger than the burst can never be satisfied; reject defensively.
	_, err := l.acquireBytes(t.Context(), 2000)
	require.Error(t, err)
	require.Equal(t, grpccodes.ResourceExhausted, status.Code(err))
}

func TestUploadLimiterContextCancelWhileWaiting(t *testing.T) {
	// With a large maxWait the request parks on the timer; cancelling ctx
	// releases it with ctx.Err() rather than ResourceExhausted.
	const rate, burst = 1000, 1000
	l, _ := newTestLimiter(t, rate, burst, 60*time.Second, 4)

	ctx, cancel := context.WithCancel(t.Context())
	_, err := l.acquireBytes(ctx, burst) // drain so the next call must wait
	require.NoError(t, err)

	resCh := make(chan error, 1)
	go func() {
		_, err := l.acquireBytes(ctx, 500)
		resCh <- err
	}()

	// The goroutine is parked on the (unadvanced) mock timer. Cancelling makes
	// the select take the ctx.Done() branch.
	cancel()
	select {
	case err := <-resCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("acquireBytes did not return after context cancel")
	}
}

func TestUploadLimiterInFlightCap(t *testing.T) {
	l, _ := newTestLimiter(t, 1000, 1000, 100*time.Millisecond, 2)

	r1 := l.acquireSlot()
	require.NotNil(t, r1)
	r2 := l.acquireSlot()
	require.NotNil(t, r2)

	// Cap of 2 is full: the third acquire fails immediately.
	r3 := l.acquireSlot()
	require.Nil(t, r3)

	// Releasing one frees a slot for the next acquire.
	r1()
	r4 := l.acquireSlot()
	require.NotNil(t, r4)

	r2()
	r4()
}
