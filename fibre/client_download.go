package fibre

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	"github.com/celestiaorg/celestia-app/v8/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	// ErrNotFound is returned when no shards were retrieved for the blob.
	ErrNotFound = errors.New("blob not found: no shards retrieved")
	// ErrNotEnoughShards is returned when not enough shards were retrieved to reconstruct the blob.
	ErrNotEnoughShards = errors.New("not enough shards to reconstruct blob")
)

// Download retrieves and reconstructs [Blob] by [Commitment] and additionally height from the [Server]s.
//
// The algorithm selects validators shuffled by stake weight for load balancing
// and requests them for shards. It tracks unique rows collected and dynamically
// launches more validators as needed until enough rows are collected for reconstruction.
// If any requests fail, more validators are contacted automatically.
//
// Errors:
//   - [ErrNotFound]: no shard was retrieved for the blob
//   - [ErrNotEnoughShards]: not enough shards were retrieved to reconstruct the original data
//   - [ErrBlobCommitmentMismatch]: the commitment doesn't match the reconstructed blob
func (c *Client) Download(ctx context.Context, id BlobID, height *uint64) (blob *Blob, err error) {
	if !c.started.Load() {
		return nil, errors.New("fibre client is not started")
	}
	if c.closed.Load() {
		return nil, ErrClientClosed
	}

	ctx, span := c.tracer.Start(ctx, "fibre.Client.Download",
		trace.WithAttributes(attribute.String("blob_commitment", id.Commitment().String())),
	)
	defer span.End()

	downloadDone := c.metrics.observeDownload(ctx)
	defer func() { downloadDone(blob, err) }()

	c.log.DebugContext(ctx, "initiating blob download", "blob_commitment", id.Commitment())

	// most of the times we probably should have the height, so we get the exact height
	// but if we don't the current head validator set will mostly have the same stakes
	// and if not this still won't affect correctness, just the amount of nodes we contact
	var valSet validator.Set
	if height != nil && *height > 0 {
		valSet, err = c.state.GetByHeight(ctx, *height)
	} else {
		valSet, err = c.state.Head(ctx)
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get validator set")
		return nil, fmt.Errorf("getting validator set: %w", err)
	}
	span.AddEvent("got_validator_set", trace.WithAttributes(
		attribute.Int("validator_count", len(valSet.Validators)),
		attribute.Int64("validator_set_height", int64(valSet.Height)),
	))

	blob, err = c.downloadBlob(ctx, valSet, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to download")
		return nil, err
	}

	err = blob.Reconstruct()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to reconstruct")
		return nil, fmt.Errorf("reconstructing data: %w", err)
	}

	c.metrics.downloadBytes.Add(ctx, int64(blob.DataSize()))
	c.log.DebugContext(ctx, "blob download completed successfully",
		"blob_commitment", id.Commitment(),
		"upload_size", blob.UploadSize(),
		"data_size", blob.DataSize(),
		"row_size", blob.RowSize(),
	)
	span.AddEvent("reconstructed", trace.WithAttributes(
		attribute.Int("data_size", len(blob.Data())),
		attribute.Int("row_size", blob.RowSize()),
	))
	span.SetStatus(codes.Ok, "")
	return blob, nil
}

// downloadFrom downloads a shard for a blob from a single validator, verifies the rows,
// and returns only valid ones. Rows are not applied to the blob; the caller (coordinator)
// is responsible for that.
func (c *Client) downloadFrom(
	ctx context.Context,
	val *core.Validator,
	blob *Blob,
	id BlobID,
) (rows []*rsema1d.RowInclusionProof, err error) {
	log := c.log.With("validator", val.Address.String(), "blob_commitment", id.Commitment())

	downloadStart := time.Now()
	valAddrStr := val.Address.String()

	ctx, span := c.tracer.Start(ctx, "download_from",
		trace.WithAttributes(attribute.String("validator_address", valAddrStr)),
	)
	defer span.End()

	defer func() {
		success := err == nil || context.Cause(ctx) == errDownloaded
		c.metrics.observeDownloadFrom(ctx, downloadStart, success, valAddrStr)
	}()

	client, err := c.clientCache.GetClient(ctx, val)
	if err != nil {
		if context.Cause(ctx) == errDownloaded {
			span.SetStatus(codes.Ok, "")
			return nil, err
		}
		log.WarnContext(ctx, "can't get grpc.FibreClient", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "can't get grpc.FibreClient")
		return nil, err
	}
	span.AddEvent("client_acquired")

	rpcStart := time.Now()
	resp, err := client.DownloadShard(ctx, &types.DownloadShardRequest{BlobId: id})
	c.metrics.observeDownloadFromRPC(ctx, rpcStart, err == nil || context.Cause(ctx) == errDownloaded, valAddrStr)
	if err != nil {
		if context.Cause(ctx) == errDownloaded {
			span.SetStatus(codes.Ok, "")
			return nil, err
		}
		log.WarnContext(ctx, "failed to download shard", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to download shard")
		return nil, err
	}
	rows, err = parseShard(resp.GetShard())
	if err != nil {
		log.WarnContext(ctx, "failed to parse shard", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to parse shard")
		return nil, err
	}
	var rowSize int
	if len(rows) > 0 && len(rows[0].Row) > 0 {
		rowSize = len(rows[0].Row)
	}
	span.AddEvent("rows_received", trace.WithAttributes(
		attribute.Int("row_count", len(rows)),
		attribute.Int("row_size", rowSize),
	))

	verified := make([]*rsema1d.RowInclusionProof, 0, len(rows))
	for _, row := range rows {
		if err := blob.VerifyRow(row); err != nil {
			log.WarnContext(ctx, "invalid row", "row_index", row.Index, "error", err)
			span.AddEvent("invalid_row", trace.WithAttributes(
				attribute.Int("row_index", row.Index),
				attribute.String("error", err.Error()),
			))
			continue
		}
		verified = append(verified, row)
	}

	if len(verified) == 0 {
		log.WarnContext(ctx, "no valid rows from validator", "rows_total", len(rows), "row_size", rowSize)
		span.SetStatus(codes.Error, "no valid rows")
		return nil, fmt.Errorf("no valid rows from validator %s", val.Address)
	}

	log.DebugContext(ctx, "got rows", "rows_total", len(rows), "verified", len(verified), "row_size", rowSize)
	span.SetStatus(codes.Ok, "")
	return verified, nil
}

// downloadResult holds the result of a single validator shard download.
type downloadResult struct {
	valIdx int
	rows   []*rsema1d.RowInclusionProof
	err    error
}

// downloadBlob downloads shards from validators concurrently and populates the blob.
// It tracks unique rows collected and dynamically launches more validators as needed,
// applying rows single-threaded in the coordinator goroutine.
func (c *Client) downloadBlob(
	ctx context.Context,
	valSet validator.Set,
	id BlobID,
) (*Blob, error) {
	ctx, span := c.tracer.Start(ctx, "fibre.Client.downloadBlob")
	defer span.End()

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(errDownloaded)

	blob, err := NewEmptyBlob(id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create empty blob")
		return nil, fmt.Errorf("creating empty blob: %w", err)
	}

	blobCfg := blob.Config()
	originalRows := blobCfg.OriginalRows

	// Get validators in priority order (shuffled by stake for load balancing)
	// Each SelectedValidator includes ExpectedRows for inflight estimation.
	selected := valSet.Select(originalRows, c.Config.MinRowsPerValidator, c.Config.LivenessThreshold)
	resultCh := make(chan downloadResult, len(selected))

	var (
		uniqueRows   int
		inflightRows int
		nextVal      int
		active       int
	)

loop:
	for {
		// Determine if we need more validators to cover originalRows
		needMore := uniqueRows+inflightRows < originalRows && nextVal < len(selected)

		// Use nil-channel trick: only select on semaphore when we need more validators
		var semCh chan struct{}
		if needMore {
			semCh = c.downloadSem
		}

		// Nothing more to do: no inflight requests and no more validators to try
		if !needMore && active == 0 {
			break
		}

		select {
		case semCh <- struct{}{}:
			// Acquired semaphore slot, launch fetch goroutine
			valIdx := nextVal
			sv := selected[valIdx]
			nextVal++
			inflightRows += sv.ExpectedRows
			active++

			c.closeWg.Add(1)
			go func() {
				defer func() {
					<-c.downloadSem
					c.closeWg.Done()
				}()

				rows, err := c.downloadFrom(ctx, sv.Validator, blob, id)
				resultCh <- downloadResult{valIdx: valIdx, rows: rows, err: err}
			}()

		case res := <-resultCh:
			active--
			inflightRows -= selected[res.valIdx].ExpectedRows

			if res.err != nil {
				c.log.WarnContext(ctx, "shard fetch failed",
					"validator", selected[res.valIdx].Address,
					"error", res.err,
				)
				continue
			}

			// Rows are already verified in downloadFrom; just assign to blob
			var applied int
			for _, row := range res.rows {
				if blob.SetRow(row) {
					applied++
				}
			}
			uniqueRows += applied
			span.AddEvent("rows_applied", trace.WithAttributes(
				attribute.Int("applied", applied),
				attribute.Int("unique_rows", uniqueRows),
				attribute.Int("original_rows", originalRows),
				attribute.String("validator", selected[res.valIdx].Address.String()),
			))

			if uniqueRows >= originalRows {
				break loop
			}

		case <-ctx.Done():
			span.RecordError(ctx.Err())
			span.SetStatus(codes.Error, "context cancelled")
			return nil, ctx.Err()
		}
	}

	switch {
	case uniqueRows == 0:
		span.SetStatus(codes.Error, "no shards retrieved")
		return nil, ErrNotFound
	case uniqueRows < originalRows:
		span.SetStatus(codes.Error, "not enough shards")
		return nil, ErrNotEnoughShards
	default:
		span.SetStatus(codes.Ok, "")
		return blob, nil
	}
}

// errDownloaded signals that context was cancelled because download completed successfully.
var errDownloaded = errors.New("downloaded")

// parseShard extracts rows from the BlobShard response, constructing RowInclusionProofs.
func parseShard(shard *types.BlobShard) ([]*rsema1d.RowInclusionProof, error) {
	if shard == nil {
		return nil, fmt.Errorf("shard response is nil")
	}

	rowsArray := shard.GetRows()
	if len(rowsArray) == 0 {
		return nil, fmt.Errorf("no rows in shard")
	}

	if len(shard.GetRoot()) != 32 {
		return nil, fmt.Errorf("invalid RLC root length: expected 32 bytes, got %d", len(shard.GetRoot()))
	}

	var rlcRoot [32]byte
	copy(rlcRoot[:], shard.GetRoot())

	proofs := make([]*rsema1d.RowInclusionProof, 0, len(rowsArray))
	for _, row := range rowsArray {
		if row == nil {
			continue
		}
		proofs = append(proofs, &rsema1d.RowInclusionProof{
			RowProof: rsema1d.RowProof{
				Index:    int(row.Index),
				Row:      row.Data,
				RowProof: row.Proof,
			},
			RLCRoot: rlcRoot,
		})
	}

	return proofs, nil
}
