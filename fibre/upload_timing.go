package fibre

import (
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Aggregate fanout timing avoids a metric label for every validator or blob.
type uploadFanoutTiming struct {
	span              trace.Span
	successes         atomic.Int64
	failures          atomic.Int64
	proofNanos        atomic.Int64
	rpcNanos          atomic.Int64
	rpcCalls          atomic.Int64
	rpcDataBytes      atomic.Int64
	retries           atomic.Int64
	maxValidatorNanos atomic.Int64
}

func newUploadFanoutTiming(span trace.Span) *uploadFanoutTiming {
	return &uploadFanoutTiming{span: span}
}

func (t *uploadFanoutTiming) recordValidator(elapsed time.Duration, success bool) {
	if success {
		t.successes.Add(1)
	} else {
		t.failures.Add(1)
	}
	for old := t.maxValidatorNanos.Load(); int64(elapsed) > old; old = t.maxValidatorNanos.Load() {
		if t.maxValidatorNanos.CompareAndSwap(old, int64(elapsed)) {
			break
		}
	}
}

func (t *uploadFanoutTiming) end() {
	t.span.SetAttributes(
		attribute.Int64("validators_succeeded", t.successes.Load()),
		attribute.Int64("validators_failed", t.failures.Load()),
		attribute.Int64("rpc_calls", t.rpcCalls.Load()),
		attribute.Int64("rpc_data_bytes", t.rpcDataBytes.Load()),
		attribute.Int64("retries", t.retries.Load()),
		attribute.Float64("proof_sum_ms", float64(t.proofNanos.Load())/float64(time.Millisecond)),
		attribute.Float64("rpc_sum_ms", float64(t.rpcNanos.Load())/float64(time.Millisecond)),
		attribute.Float64("validator_max_ms", float64(t.maxValidatorNanos.Load())/float64(time.Millisecond)),
	)
	t.span.AddEvent("upload_background_done")
	t.span.End()
}
