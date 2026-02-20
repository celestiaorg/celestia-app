package fibre

import (
	"context"
	"errors"
	"fmt"

	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DownloadShard handles the [types.FibreServer.DownloadShard] RPC call.
// It retrieves [types.BlobShard] for the given commitment.
func (s *Server) DownloadShard(ctx context.Context, req *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	ctx, span := s.tracer.Start(ctx, "fibre.Server.DownloadShard")
	defer span.End()

	// unmarshal and validate commitment
	var commitment Commitment
	if err := commitment.UnmarshalBinary(req.Commitment); err != nil {
		s.log.ErrorContext(ctx, "invalid commitment", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid commitment")
		return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("invalid commitment: %v", err))
	}

	// retrieve blob shard from storage
	blobShard, err := s.store.Get(ctx, commitment)
	if err != nil {
		if errors.Is(err, ErrStoreNotFound) {
			s.log.WarnContext(ctx, "no blob shard found for commitment", "blob_commitment", commitment.String())
			span.SetStatus(codes.Error, "no blob shard found")
			return nil, status.Error(grpccodes.NotFound, fmt.Sprintf("no blob shard found for commitment %s", commitment.String()))
		}
		s.log.ErrorContext(ctx, "failed to retrieve blob shard", "blob_commitment", commitment.String(), "error", err)
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

	s.log.InfoContext(ctx, "download successful",
		"blob_commitment", commitment.String(),
		"rows", len(blobShard.Rows),
		"row_size", rowSize,
	)

	span.SetStatus(codes.Ok, "")
	return &types.DownloadShardResponse{
		Shard: blobShard,
	}, nil
}
