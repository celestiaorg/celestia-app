package fibre

import (
	"context"
	"time"

	clock "github.com/filecoin-project/go-clock"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// uploadLimiter is the server-side admission controller for UploadShard. It
// bounds two things: the number of in-flight handlers admitted past
// verification (a memory backstop), and the upload byte throughput charged as
// the whole-blob PaymentPromise.UploadSize (an approximation of the network's
// global PFF blob-admission rate).
//
// The controller is disabled as a unit when UploadRateLimitEnabled is false or
// the configured rate is <= 0, in which case every method is a cheap no-op.
type uploadLimiter struct {
	enabled bool
	clk     clock.Clock
	bytes   *rate.Limiter // tokens are bytes; nil when disabled
	maxWait time.Duration
	slots   chan struct{} // in-flight semaphore, cap == MaxUploadShardInFlight; nil when disabled
	metrics *serverMetrics
}

// newUploadLimiter builds an uploadLimiter from a validated [ServerConfig].
// It returns a disabled limiter unless the controller is active (enabled toggle
// AND a positive rate). The max-wait string is assumed valid (checked by
// [ServerConfig.Validate]); a parse failure falls back to no wait.
func newUploadLimiter(cfg ServerConfig, m *serverMetrics) *uploadLimiter {
	if !cfg.uploadRateLimitActive() {
		return &uploadLimiter{enabled: false}
	}
	maxWait, _ := cfg.uploadRateLimitMaxWait()
	return &uploadLimiter{
		enabled: true,
		clk:     cfg.Clock,
		bytes:   rate.NewLimiter(rate.Limit(cfg.UploadRateLimitBytesPerSecond), cfg.UploadRateLimitBurstBytes),
		maxWait: maxWait,
		slots:   make(chan struct{}, cfg.MaxUploadShardInFlight),
		metrics: m,
	}
}

// acquireSlot takes an in-flight slot without blocking. It returns a release
// func to be deferred by the caller, or nil if the in-flight cap is full (the
// caller should reject with ResourceExhausted). When disabled it returns a
// no-op release.
func (l *uploadLimiter) acquireSlot() func() {
	if !l.enabled {
		return func() {}
	}
	select {
	case l.slots <- struct{}{}:
		return func() { <-l.slots }
	default:
		return nil
	}
}

// acquireBytes reserves n bytes of upload throughput, waiting up to maxWait and
// no longer than the request context. It returns the time spent waiting for
// budget (so callers can exclude it from end-to-end latency metrics) and:
//   - nil when admitted (immediately or after a bounded wait);
//   - a ResourceExhausted status error when the required wait exceeds maxWait
//     (or n exceeds the bucket burst, which the default config prevents);
//   - ctx.Err() when the context ends before admission.
//
// When disabled it admits any size immediately with zero wait.
func (l *uploadLimiter) acquireBytes(ctx context.Context, n int) (time.Duration, error) {
	if !l.enabled {
		return 0, nil
	}

	now := l.clk.Now()
	r := l.bytes.ReserveN(now, n)
	if !r.OK() {
		// n exceeds the bucket burst: it can never be satisfied. With the default
		// burst (>= max blob size) this is unreachable, but reject defensively
		// rather than block forever.
		return 0, status.Errorf(grpccodes.ResourceExhausted, "upload size %d exceeds rate limiter burst", n)
	}

	delay := r.DelayFrom(now)
	if delay <= 0 {
		l.metrics.uploadAdmittedBytes.Add(ctx, int64(n))
		return 0, nil
	}
	if delay > l.maxWait {
		r.CancelAt(l.clk.Now())
		return 0, status.Errorf(grpccodes.ResourceExhausted, "upload rate limit exceeded: required wait %s exceeds max %s", delay, l.maxWait)
	}

	waitStart := l.clk.Now()
	timer := l.clk.Timer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		wait := l.clk.Now().Sub(waitStart)
		l.metrics.uploadRateWaitDuration.Record(ctx, wait.Seconds())
		l.metrics.uploadAdmittedBytes.Add(ctx, int64(n))
		return wait, nil
	case <-ctx.Done():
		// Return the tokens we reserved so a cancelled request doesn't starve
		// others. CancelAt only refunds when the reservation has not yet been
		// "consumed" (i.e. delay not yet elapsed), which holds here.
		now := l.clk.Now()
		r.CancelAt(now)
		return now.Sub(waitStart), ctx.Err()
	}
}

// observeRejected records a rejection by the admission controller for the given
// reason ("byte_budget" or "in_flight"). It is a no-op when disabled.
func (l *uploadLimiter) observeRejected(ctx context.Context, reason string) {
	if !l.enabled {
		return
	}
	l.metrics.uploadRateLimited.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}
