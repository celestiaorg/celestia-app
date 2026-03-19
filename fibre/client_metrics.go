package fibre

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// clientMetrics holds OTel metric instruments for the Fibre [Client].
type clientMetrics struct {
	// Upload
	uploadInFlight      metric.Int64UpDownCounter
	uploadDuration      metric.Float64Histogram
	uploadBytes         metric.Int64Counter
	uploadDataBytes     metric.Int64Counter
	uploadNetworkBytes  metric.Int64Counter
	uploadSigsCollected metric.Int64Histogram

	// Per-validator upload
	uploadToDuration   metric.Float64Histogram
	uploadToRPCLatency metric.Float64Histogram

	// Download
	downloadInFlight metric.Int64UpDownCounter
	downloadDuration metric.Float64Histogram
	downloadBytes    metric.Int64Counter

	// Per-validator download
	downloadFromDuration   metric.Float64Histogram
	downloadFromRPCLatency metric.Float64Histogram
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

	cm.uploadBytes, err = m.Int64Counter("fibre.client.upload.bytes",
		metric.WithDescription("Total bytes uploaded (original rows with padding, without parity)"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating upload bytes counter: %w", err)
	}

	cm.uploadDataBytes, err = m.Int64Counter("fibre.client.upload.data_bytes",
		metric.WithDescription("Total original data bytes uploaded (without padding or coding overhead)"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating upload data bytes counter: %w", err)
	}

	cm.uploadNetworkBytes, err = m.Int64Counter("fibre.client.upload.network_bytes",
		metric.WithDescription("Total bytes pushed to all validators (includes parity and shard duplication)"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating upload network bytes counter: %w", err)
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

	cm.downloadBytes, err = m.Int64Counter("fibre.client.download.bytes",
		metric.WithDescription("Total bytes downloaded"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating download bytes counter: %w", err)
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

	return &cm, nil
}

// observeUpload records in-flight increment and returns a function that records
// the upload duration and decrements in-flight. Call the returned function in a defer.
func (m *clientMetrics) observeUpload(ctx context.Context, blobSize int) (done func(err error)) {
	start := time.Now()
	m.uploadInFlight.Add(ctx, 1)
	return func(err error) {
		m.uploadInFlight.Add(ctx, -1)
		m.uploadDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attribute.Int("blob_size", blobSize),
			attribute.Bool("success", err == nil),
		))
	}
}

// observeUploadComplete records byte counters and signature count after a successful upload.
func (m *clientMetrics) observeUploadComplete(ctx context.Context, uploadSize, dataSize, networkBytes, sigsCollected int) {
	m.uploadBytes.Add(ctx, int64(uploadSize))
	m.uploadDataBytes.Add(ctx, int64(dataSize))
	m.uploadNetworkBytes.Add(ctx, int64(networkBytes))
	m.uploadSigsCollected.Record(ctx, int64(sigsCollected))
}

// observeUploadTo records per-validator upload duration.
func (m *clientMetrics) observeUploadTo(ctx context.Context, start time.Time, success bool, blobSize int, valAddr string) {
	m.uploadToDuration.Record(
		ctx,
		time.Since(start).Seconds(), metric.WithAttributes(
			attribute.Bool("success", success),
			attribute.Int("blob_size", blobSize),
			attribute.String("validator_address", valAddr),
		))
}

// observeUploadToRPC records per-validator RPC latency.
func (m *clientMetrics) observeUploadToRPC(ctx context.Context, start time.Time, success bool, valAddr string) {
	m.uploadToRPCLatency.Record(
		ctx,
		time.Since(start).Seconds(), metric.WithAttributes(
			attribute.Bool("success", success),
			attribute.String("validator_address", valAddr),
		))
}

// observeDownload records in-flight increment and returns a function that records
// the download duration and decrements in-flight.
func (m *clientMetrics) observeDownload(ctx context.Context) (done func(blob *Blob, err error)) {
	start := time.Now()
	m.downloadInFlight.Add(ctx, 1)
	return func(blob *Blob, err error) {
		m.downloadInFlight.Add(ctx, -1)
		attrs := []attribute.KeyValue{attribute.Bool("success", err == nil)}
		if blob != nil {
			attrs = append(attrs, attribute.Int("blob_size", blob.DataSize()))
		}
		m.downloadDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
	}
}

// observeDownloadFrom records per-validator download duration.
func (m *clientMetrics) observeDownloadFrom(ctx context.Context, start time.Time, success bool, valAddr string) {
	m.downloadFromDuration.Record(
		ctx,
		time.Since(start).Seconds(), metric.WithAttributes(
			attribute.Bool("success", success),
			attribute.String("validator_address", valAddr),
		))
}

// observeDownloadFromRPC records per-validator download RPC latency.
func (m *clientMetrics) observeDownloadFromRPC(ctx context.Context, start time.Time, success bool, valAddr string) {
	m.downloadFromRPCLatency.Record(
		ctx,
		time.Since(start).Seconds(), metric.WithAttributes(
			attribute.Bool("success", success),
			attribute.String("validator_address", valAddr),
		))
}
