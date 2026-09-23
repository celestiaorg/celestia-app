package fibre

import (
	"context"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"time"
)

type uploadPhaseMetrics struct {
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
	bytes    metric.Int64UpDownCounter
}

func newUploadPhaseMetrics(m metric.Meter) (*uploadPhaseMetrics, error) {
	if _, err := m.Int64ObservableGauge("fibre.server.storage.packed.admission_capacity", metric.WithUnit("By"), metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error { o.Observe(packedAdmissionBytes); return nil })); err != nil {
		return nil, err
	}
	p := &uploadPhaseMetrics{}
	var err error
	p.duration, err = m.Float64Histogram("fibre.server.upload.phase.duration", metric.WithUnit("s"), metric.WithDescription("Upload phase wall time, including waits; phases may overlap"), metric.WithExplicitBucketBoundaries(.0001, .001, .01, .1, .25, .5, 1, 2, 5, 10, 20, 30, 60, 120, 300))
	if err != nil {
		return nil, err
	}
	p.active, err = m.Int64UpDownCounter("fibre.server.upload.phase.active", metric.WithDescription("Operations currently in each upload phase"))
	if err != nil {
		return nil, err
	}
	p.bytes, err = m.Int64UpDownCounter("fibre.server.upload.phase.bytes", metric.WithUnit("By"), metric.WithDescription("Payload bytes currently in each upload phase; phases may overlap"))
	return p, err
}

// phase uses only static phase names, never request identifiers.
func (m *serverMetrics) phase(ctx context.Context, name string, bytes int64) func() {
	if m == nil || m.phases == nil {
		return func() {}
	}
	p := m.phases
	attrs := metric.WithAttributes(attribute.String("phase", name))
	start := time.Now()
	p.active.Add(ctx, 1, attrs)
	if bytes != 0 {
		p.bytes.Add(ctx, bytes, attrs)
	}
	return func() {
		p.active.Add(ctx, -1, attrs)
		if bytes != 0 {
			p.bytes.Add(ctx, -bytes, attrs)
		}
		p.duration.Record(ctx, time.Since(start).Seconds(), attrs)
	}
}
