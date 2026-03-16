package fibre

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DownloadShard handles the [types.FibreServer.DownloadShard] RPC call.
// It retrieves [types.BlobShard] for the given blob ID.
func (s *Server) DownloadShard(ctx context.Context, req *types.DownloadShardRequest) (_ *types.DownloadShardResponse, err error) {
	start := time.Now()
	var shardSize int64
	s.metrics.downloadShardInFlight.Add(ctx, 1)

	ctx, span := s.tracer.Start(ctx, "fibre.Server.DownloadShard")
	defer span.End()
	defer func() {
		s.metrics.downloadShardInFlight.Add(ctx, -1)
		attrs := []attribute.KeyValue{
			attribute.Int64("shard_size", shardSize),
			attribute.Bool("success", err == nil),
		}
		s.metrics.downloadShardDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
	}()

	// unmarshal and validate blob ID
	var id BlobID
	if err := id.UnmarshalBinary(req.BlobId); err != nil {
		s.log.ErrorContext(ctx, "invalid blob ID", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid blob ID")
		return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("invalid blob ID: %v", err))
	}

	// validate blob version is supported
	if _, err := BlobConfigForVersion(id.Version()); err != nil {
		s.log.ErrorContext(ctx, "unsupported blob version", "version", id.Version(), "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "unsupported blob version")
		return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("unsupported blob version: %v", err))
	}

	// retrieve blob shard from storage using commitment
	storeGetStart := time.Now()
	blobShard, err := s.store.Get(ctx, id.Commitment())
	s.metrics.storeGetDuration.Record(ctx, time.Since(storeGetStart).Seconds(), metric.WithAttributes(attribute.Bool("success", err == nil)))
	if err != nil {
		if errors.Is(err, ErrStoreNotFound) {
			s.log.WarnContext(ctx, "no blob shard found for commitment", "blob_commitment", id.Commitment().String())
			span.SetStatus(codes.Error, "no blob shard found")
			return nil, status.Error(grpccodes.NotFound, fmt.Sprintf("no blob shard found for commitment %s", id.Commitment().String()))
		}
		s.log.ErrorContext(ctx, "failed to retrieve blob shard", "blob_commitment", id.Commitment().String(), "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to retrieve blob shard")
		return nil, status.Error(grpccodes.Internal, fmt.Sprintf("failed to retrieve blob shard: %v", err))
	}

	var rowSize int
	if len(blobShard.Rows) > 0 && len(blobShard.Rows[0].Data) > 0 {
		rowSize = len(blobShard.Rows[0].Data)
	}
	span.AddEvent("shard_read", trace.WithAttributes(
		attribute.Int("row_count", len(blobShard.Rows)),
		attribute.Int("row_size", rowSize),
	))

	for _, row := range blobShard.Rows {
		shardSize += int64(len(row.Data))
	}

	s.log.InfoContext(ctx, "download successful",
		"blob_commitment", id.Commitment().String(),
		"rows", len(blobShard.Rows),
		"row_size", rowSize,
	)

	span.SetStatus(codes.Ok, "")
	return &types.DownloadShardResponse{
		Shard: blobShard,
	}, nil
}
