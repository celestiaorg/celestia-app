package fibre

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// clientMetrics holds OTel metric instruments for the Fibre [Client].
type clientMetrics struct {
	// Upload
	uploadInFlight      metric.Int64UpDownCounter
	uploadDuration      metric.Float64Histogram
	uploadSigsCollected metric.Int64Histogram

	// Per-validator upload
	uploadToDuration   metric.Float64Histogram
	uploadToRPCLatency metric.Float64Histogram

	// Download
	downloadInFlight metric.Int64UpDownCounter
	downloadDuration metric.Float64Histogram

	// Per-validator download
	downloadFromDuration   metric.Float64Histogram
	downloadFromRPCLatency metric.Float64Histogram

	// Put (upload + tx submission)
	putInFlight metric.Int64UpDownCounter
	putDuration metric.Float64Histogram
}

func newClientMetrics(m metric.Meter) (*clientMetrics, error) {
	var (
		cm  clientMetrics
		err error
	)

	// Upload metrics
	cm.uploadInFlight, err = m.Int64UpDownCounter("fibre.client.upload.in_flight",
		metric.WithDescription("Number of blob uploads currently in progress"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating upload in_flight counter: %w", err)
	}

	cm.uploadDuration, err = m.Float64Histogram("fibre.client.upload.duration",
		metric.WithDescription("Duration of blob uploads in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)
	if err != nil {
		return nil, fmt.Errorf("creating upload duration histogram: %w", err)
	}

	cm.uploadSigsCollected, err = m.Int64Histogram("fibre.client.upload.signatures_collected",
		metric.WithDescription("Number of validator signatures collected per upload"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 20, 34, 50, 67, 100),
	)
	if err != nil {
		return nil, fmt.Errorf("creating upload sigs collected histogram: %w", err)
	}

	// Per-validator upload metrics
	cm.uploadToDuration, err = m.Float64Histogram("fibre.client.upload_to.duration",
		metric.WithDescription("Duration of per-validator UploadShard operations in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("creating upload_to duration histogram: %w", err)
	}

	cm.uploadToRPCLatency, err = m.Float64Histogram("fibre.client.upload_to.rpc_latency",
		metric.WithDescription("Per-validator UploadShard RPC network latency in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("creating upload_to rpc_latency histogram: %w", err)
	}

	// Download metrics
	cm.downloadInFlight, err = m.Int64UpDownCounter("fibre.client.download.in_flight",
		metric.WithDescription("Number of blob downloads currently in progress"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating download in_flight counter: %w", err)
	}

	cm.downloadDuration, err = m.Float64Histogram("fibre.client.download.duration",
		metric.WithDescription("Duration of blob downloads in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)
	if err != nil {
		return nil, fmt.Errorf("creating download duration histogram: %w", err)
	}

	// Per-validator download metrics
	cm.downloadFromDuration, err = m.Float64Histogram("fibre.client.download_from.duration",
		metric.WithDescription("Duration of per-validator DownloadShard operations in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("creating download_from duration histogram: %w", err)
	}

	cm.downloadFromRPCLatency, err = m.Float64Histogram("fibre.client.download_from.rpc_latency",
		metric.WithDescription("Per-validator DownloadShard RPC network latency in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("creating download_from rpc_latency histogram: %w", err)
	}

	// Put metrics
	cm.putInFlight, err = m.Int64UpDownCounter("fibre.client.put.in_flight",
		metric.WithDescription("Number of Put operations currently in progress"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating put in_flight counter: %w", err)
	}

	cm.putDuration, err = m.Float64Histogram("fibre.client.put.duration",
		metric.WithDescription("Duration of Put operations in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2.5, 5, 10, 30, 60),
	)
	if err != nil {
		return nil, fmt.Errorf("creating put duration histogram: %w", err)
	}

	return &cm, nil
}

// serverMetrics holds OTel metric instruments for the Fibre [Server].
type serverMetrics struct {
	// UploadShard RPC
	uploadShardInFlight metric.Int64UpDownCounter
	uploadShardDuration metric.Float64Histogram

	// DownloadShard RPC
	downloadShardInFlight metric.Int64UpDownCounter
	downloadShardDuration metric.Float64Histogram

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
