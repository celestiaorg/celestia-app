package fibre

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// serverMetrics holds OTel metric instruments for the Fibre [Server].
type serverMetrics struct {
	// UploadShard RPC
	uploadShardInFlight metric.Int64UpDownCounter
	uploadShardDuration metric.Float64Histogram
	uploadShardBytes    metric.Int64Counter

	// DownloadShard RPC
	downloadShardInFlight metric.Int64UpDownCounter
	downloadShardDuration metric.Float64Histogram
	downloadShardBytes    metric.Int64Counter

	// Store operations
	storePutDuration metric.Float64Histogram
	storeGetDuration metric.Float64Histogram

	// Signing
	signDuration metric.Float64Histogram

	// Prune
	pruneEntries  metric.Int64Counter
	pruneDuration metric.Float64Histogram
}

func newServerMetrics(m metric.Meter) (*serverMetrics, error) {
	var (
		sm  serverMetrics
		err error
	)

	// UploadShard RPC metrics
	sm.uploadShardInFlight, err = m.Int64UpDownCounter("fibre.server.upload_shard.in_flight",
		metric.WithDescription("Number of UploadShard RPCs currently being handled"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating upload_shard in_flight counter: %w", err)
	}

	sm.uploadShardDuration, err = m.Float64Histogram("fibre.server.upload_shard.duration",
		metric.WithDescription("Duration of UploadShard RPCs in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		return nil, fmt.Errorf("creating upload_shard duration histogram: %w", err)
	}

	sm.uploadShardBytes, err = m.Int64Counter("fibre.server.upload_shard.bytes",
		metric.WithDescription("Total bytes received via UploadShard RPCs"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating upload_shard bytes counter: %w", err)
	}

	// DownloadShard RPC metrics
	sm.downloadShardInFlight, err = m.Int64UpDownCounter("fibre.server.download_shard.in_flight",
		metric.WithDescription("Number of DownloadShard RPCs currently being handled"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating download_shard in_flight counter: %w", err)
	}

	sm.downloadShardDuration, err = m.Float64Histogram("fibre.server.download_shard.duration",
		metric.WithDescription("Duration of DownloadShard RPCs in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		return nil, fmt.Errorf("creating download_shard duration histogram: %w", err)
	}

	sm.downloadShardBytes, err = m.Int64Counter("fibre.server.download_shard.bytes",
		metric.WithDescription("Total bytes sent via DownloadShard RPCs"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating download_shard bytes counter: %w", err)
	}

	// Store operation metrics
	sm.storePutDuration, err = m.Float64Histogram("fibre.server.store.put.duration",
		metric.WithDescription("Duration of store Put operations in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	)
	if err != nil {
		return nil, fmt.Errorf("creating store put duration histogram: %w", err)
	}

	sm.storeGetDuration, err = m.Float64Histogram("fibre.server.store.get.duration",
		metric.WithDescription("Duration of store Get operations in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	)
	if err != nil {
		return nil, fmt.Errorf("creating store get duration histogram: %w", err)
	}

	// Signing metrics
	sm.signDuration, err = m.Float64Histogram("fibre.server.sign.duration",
		metric.WithDescription("Duration of payment promise signing in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1),
	)
	if err != nil {
		return nil, fmt.Errorf("creating sign duration histogram: %w", err)
	}

	// Prune metrics
	sm.pruneEntries, err = m.Int64Counter("fibre.server.prune.entries",
		metric.WithDescription("Total entries pruned"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating prune entries counter: %w", err)
	}

	sm.pruneDuration, err = m.Float64Histogram("fibre.server.prune.duration",
		metric.WithDescription("Duration of prune cycles in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.01, 0.05, 0.1, 0.5, 1, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("creating prune duration histogram: %w", err)
	}

	return &sm, nil
}

// observeUploadShard records in-flight increment and returns a function that records
// duration and decrements in-flight. Call the returned function in a defer.
func (m *serverMetrics) observeUploadShard(ctx context.Context) (done func(uploadSize int64, err error)) {
	start := time.Now()
	m.uploadShardInFlight.Add(ctx, 1)
	return func(uploadSize int64, err error) {
		m.uploadShardInFlight.Add(ctx, -1)
		attrs := []attribute.KeyValue{attribute.Bool("success", err == nil)}
		if uploadSize > 0 {
			attrs = append(attrs, attribute.Int64("upload_size", uploadSize))
		}
		m.uploadShardDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
	}
}

// observeDownloadShard records in-flight increment and returns a function that records
// duration and decrements in-flight. Call the returned function in a defer.
func (m *serverMetrics) observeDownloadShard(ctx context.Context) (done func(shardSize int64, err error)) {
	start := time.Now()
	m.downloadShardInFlight.Add(ctx, 1)
	return func(shardSize int64, err error) {
		m.downloadShardInFlight.Add(ctx, -1)
		attrs := []attribute.KeyValue{attribute.Bool("success", err == nil)}
		if shardSize > 0 {
			attrs = append(attrs, attribute.Int64("shard_size", shardSize))
		}
		m.downloadShardDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
	}
}

// observeStoreOp records store operation duration.
func (m *serverMetrics) observeStoreOp(ctx context.Context, h metric.Float64Histogram, start time.Time, success bool) {
	h.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.Bool("success", success)))
}

// observeSign records signing duration.
func (m *serverMetrics) observeSign(ctx context.Context, start time.Time, success bool) {
	m.signDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.Bool("success", success)))
}

// observePrune records prune cycle duration and entries pruned.
func (m *serverMetrics) observePrune(ctx context.Context, start time.Time, pruned int, err error) {
	m.pruneDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.Bool("success", err == nil)))
	if pruned > 0 && err == nil {
		m.pruneEntries.Add(ctx, int64(pruned))
	}
}
