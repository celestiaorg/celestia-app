package fibre

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
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

// Download retrieves and reconstructs [Blob] by [Commitment] from the [Server]s.
//
// The algorithm selects minimal required number of validators,
// shuffled by stake weight for load balancing and requests them for shards.
// If any of the requests fails, more validators are requested until enough shards are retrieved or
// the maximum number of validators is reached. In the happy case, the operation succeeds in a single roundtrip.
//
// Errors:
//   - [ErrNotFound]: no shard was retrieved for the blob
//   - [ErrNotEnoughShards]: not enough shards were retrieved to reconstruct the original data
//   - [ErrBlobCommitmentMismatch]: the commitment doesn't match the reconstructed blob
func (c *Client) Download(ctx context.Context, id BlobID) (blob *Blob, err error) {
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

	// get validator set
	// TODO(@Wondertan): If we don't want to pass height here, we should at least ensure we handle the case
	// where the most recent validator set is different from the one the data was posted at somehow.
	valSet, err := c.state.Head(ctx)
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

// downloadFrom downloads a shard for a blob from a single validator and applies its rows to the blob.
func (c *Client) downloadFrom(
	ctx context.Context,
	val *core.Validator,
	blob *Blob,
) (err error) {
	log := c.log.With("validator", val.Address.String(), "blob_commitment", blob.ID().Commitment())

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
			return err
		}
		log.WarnContext(ctx, "can't get grpc.FibreClient", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "can't get grpc.FibreClient")
		return err
	}
	span.AddEvent("client_acquired")

	rpcStart := time.Now()
	resp, err := client.DownloadShard(ctx, &types.DownloadShardRequest{BlobId: blob.ID()})
	c.metrics.observeDownloadFromRPC(ctx, rpcStart, err == nil || context.Cause(ctx) == errDownloaded, valAddrStr)
	if err != nil {
		if context.Cause(ctx) == errDownloaded {
			span.SetStatus(codes.Ok, "")
			return err
		}
		log.WarnContext(ctx, "failed to download shard", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to download shard")
		return err
	}
	rows, err := parseShard(resp.GetShard())
	if err != nil {
		log.WarnContext(ctx, "failed to parse shard", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to parse shard")
		return err
	}
	var rowSize int
	if len(rows) > 0 && len(rows[0].Row) > 0 {
		rowSize = len(rows[0].Row)
	}
	span.AddEvent("rows_received", trace.WithAttributes(
		attribute.Int("row_count", len(rows)),
		attribute.Int("row_size", rowSize),
	))

	var applied int
	for _, row := range rows {
		if err := blob.SetRow(row); err != nil {
			log.WarnContext(ctx, "invalid row", "row_index", row.Index, "error", err)
			span.AddEvent("invalid_row", trace.WithAttributes(attribute.Int("row_index", row.Index)))
			continue
		}
		applied++
	}

	span.AddEvent("rows_applied", trace.WithAttributes(
		attribute.Int("applied", applied),
		attribute.Int("total", len(rows)),
		attribute.Int("row_size", rowSize),
	))
	if applied == 0 {
		log.WarnContext(ctx, "no rows applied", "rows_total", len(rows), "row_size", rowSize)
		span.SetStatus(codes.Error, "no rows applied")
		return fmt.Errorf("no rows applied from validator %s", val.Address)
	}

	log.DebugContext(ctx, "got rows", "rows_applied", applied, "rows_total", len(rows), "row_size", rowSize)
	span.SetStatus(codes.Ok, "")
	return nil
}

// downloadBlob downloads shards from validators concurrently and populates the blob.
// It requests minimally required number of validators (e.g. 2/3 by default)
// and requests further ones if any initial validator requests fail.
func (c *Client) downloadBlob(
	ctx context.Context,
	valSet validator.Set,
	id BlobID,
) (*Blob, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(errDownloaded)

	blob, err := NewEmptyBlob(id)
	if err != nil {
		return nil, fmt.Errorf("creating empty blob: %w", err)
	}

	var (
		responses            atomic.Uint32         // tracks finished responses
		responsesExhaustedCh = make(chan struct{}) // closes when all responses complete
	)

	var (
		successes    atomic.Uint32         // tracks successful responses
		downloadedCh = make(chan struct{}) // closes when downloadTarget amount of responses complete
	)

	// select validators shuffled by stake for load balancing
	validators, downloadTarget := valSet.Select(blob.Config().OriginalRows, c.Config.MinRowsPerValidator, c.Config.LivenessThreshold)
	downloadLimitCh := make(chan struct{}, downloadTarget)

loop:
	for _, val := range validators {
		// local semaphore first - order matters
		select {
		case downloadLimitCh <- struct{}{}:
		case <-downloadedCh:
			break loop
		case <-ctx.Done():
			break loop
		}

		select {
		case c.downloadSem <- struct{}{}:
		case <-downloadedCh:
			break loop
		case <-ctx.Done():
			break loop
		}

		c.closeWg.Add(1)
		go func(val *core.Validator) {
			defer func() {
				// release global semaphore
				<-c.downloadSem

				// increment responses and mark as completed if so
				if int(responses.Add(1)) == len(validators) {
					close(responsesExhaustedCh)
				}

				// unblock Close
				c.closeWg.Done()
			}()

			if err := c.downloadFrom(ctx, val, blob); err != nil {
				// release to replace this failed request with a new one
				<-downloadLimitCh
				return
			}

			// increment successes and mark download completed if so
			if successes.Add(1) == uint32(downloadTarget) {
				close(downloadedCh)
			}
		}(val)
	}

	select {
	case <-downloadedCh:
	case <-responsesExhaustedCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	s := int(successes.Load())
	switch {
	case s == 0:
		return nil, ErrNotFound
	case s < downloadTarget:
		return nil, ErrNotEnoughShards
	case s > downloadTarget:
		c.log.WarnContext(ctx, "downloaded more shards then needed", "downloaded", s, "expected_target", downloadTarget)
		fallthrough
	default:
		return blob, nil
	}
}

// errDownloaded signals that context was cancelled because download completed successfully.
var errDownloaded = errors.New("downloaded")

// parseShard extracts and validates rows from the BlobShard response, constructing RowInclusionProofs.
// Returns the row inclusion proofs with RLC root already set.
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
