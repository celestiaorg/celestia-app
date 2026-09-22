package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type txsimMetrics struct {
	blobs        metric.Int64Counter
	raw          metric.Int64Counter
	padded       metric.Int64Counter
	latency      metric.Float64Histogram
	registration metric.Registration
}

func newTxsimMetrics(meter metric.Meter, st *stats) (*txsimMetrics, error) {
	m := &txsimMetrics{}
	var err error
	m.blobs, err = meter.Int64Counter("fibre.txsim.blobs", metric.WithDescription("Blob stage outcomes; stages are not additive"))
	if err != nil {
		return nil, err
	}
	m.raw, err = meter.Int64Counter("fibre.txsim.raw_bytes", metric.WithUnit("By"))
	if err != nil {
		return nil, err
	}
	m.padded, err = meter.Int64Counter("fibre.txsim.padded_bytes", metric.WithUnit("By"))
	if err != nil {
		return nil, err
	}
	m.latency, err = meter.Float64Histogram("fibre.txsim.confirmation.duration", metric.WithUnit("s"), metric.WithDescription("Encoding start to observed confirmation outcome, including queue wait"), metric.WithExplicitBucketBoundaries(1, 2, 5, 10, 20, 30, 60, 120, 300, 600))
	if err != nil {
		return nil, err
	}
	pending, err := meter.Int64ObservableGauge("fibre.txsim.confirmation.pending")
	if err != nil {
		return nil, err
	}
	active, err := meter.Int64ObservableGauge("fibre.txsim.encoding.active")
	if err != nil {
		return nil, err
	}
	pool, err := meter.Int64ObservableGauge("fibre.txsim.pool.bytes", metric.WithUnit("By"), metric.WithDescription("Pool capacity, not RSS; mapped overlaps in_use/free"))
	if err != nil {
		return nil, err
	}
	rss, err := meter.Int64ObservableGauge("fibre.txsim.process.rss", metric.WithUnit("By"))
	if err != nil {
		return nil, err
	}
	m.registration, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		o.ObserveInt64(pending, st.pending.Load())
		o.ObserveInt64(active, st.encoding.Load())
		if resident, err := processRSS(); err == nil {
			o.ObserveInt64(rss, resident)
		}
		pools := fibre.DefaultBlobConfigV0().MemoryStats()
		for name, p := range map[string]fibre.PoolMemoryStats{"parity": pools.Parity, "trees": pools.Trees, "scratch": pools.Scratch, "downloads": pools.Downloads} {
			for state, bytes := range map[string]int64{"in_use": p.InUseBytes, "free": p.FreeBytes, "mapped": p.MappedBytes} {
				o.ObserveInt64(pool, bytes, metric.WithAttributes(attribute.String("pool", name), attribute.String("state", state)))
			}
		}
		return nil
	}, pending, active, pool, rss)
	return m, err
}

func (m *txsimMetrics) record(ctx context.Context, stage, outcome string, raw, padded int64) {
	if m == nil {
		return
	}
	opts := metric.WithAttributes(attribute.String("stage", stage), attribute.String("outcome", outcome))
	m.blobs.Add(ctx, 1, opts)
	m.raw.Add(ctx, raw, opts)
	m.padded.Add(ctx, padded, opts)
}

// Linux exposes current RSS including resident mmap pages. Other platforms omit it.
func processRSS() (int64, error) {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, err
	}
	return parseRSS(string(b), int64(os.Getpagesize()))
}

func parseRSS(statm string, pageSize int64) (int64, error) {
	fields := strings.Fields(statm)
	if len(fields) < 2 {
		return 0, fmt.Errorf("missing resident page count")
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || pageSize <= 0 || pages < 0 || pages > (1<<63-1)/pageSize {
		return 0, fmt.Errorf("invalid resident page count")
	}
	return pages * pageSize, nil
}
