package fibre

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
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

// DownloadOption configures the behavior of [Client.Download].
type DownloadOption func(*downloadOptions)

type downloadOptions struct {
	height uint64
}

// WithHeight sets the block height at which the blob was included.
// When provided, the validator set at that height is used for download;
// otherwise, the current head validator set is used.
func WithHeight(height uint64) DownloadOption {
	return func(o *downloadOptions) {
		o.height = height
	}
}

// Download retrieves and reconstructs a [Blob] by [BlobID] from the [Server]s.
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
func (c *Client) Download(ctx context.Context, id BlobID, opts ...DownloadOption) (blob *Blob, err error) {
	if !c.started.Load() {
		return nil, errors.New("fibre client is not started")
	}
	if c.closed.Load() {
		return nil, ErrClientClosed
	}

	var opt downloadOptions
	for _, o := range opts {
		o(&opt)
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
	if opt.height > 0 {
		valSet, err = c.state.GetByHeight(ctx, opt.height)
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

	blob, err = c.downloadBlob(ctx, valSet, id, false)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to download")
		return nil, err
	}

	err = blob.Reconstruct()
	if err != nil {
		if !errors.Is(err, ErrBlobCommitmentMismatch) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to reconstruct")
			return nil, fmt.Errorf("reconstructing data: %w", err)
		}

		// Commitment mismatch — retry with RLC verification to identify bad rows
		c.log.WarnContext(ctx, "reconstruction failed with commitment mismatch, retrying with RLC verification",
			"blob_commitment", id.Commitment(),
		)
		span.AddEvent("retry_with_rlc")

		blob, err = c.downloadBlob(ctx, valSet, id, true)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to download with RLC")
			return nil, err
		}

		if err = blob.Reconstruct(WithSkipCommitmentCheck()); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to reconstruct after RLC retry")
			return nil, fmt.Errorf("reconstructing data after RLC retry: %w", err)
		}
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
// is responsible for that. When withRLC is true, requests RLC coefficients from the server
// and uses them for stronger row verification.
func (c *Client) downloadFrom(
	ctx context.Context,
	val *core.Validator,
	blob *Blob,
	id BlobID,
	rlcVerifier *rlcVerifier,
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
	resp, err := client.DownloadShard(ctx, &types.DownloadShardRequest{BlobId: id, WithRlc: rlcVerifier != nil})
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
	result, err := parseShard(resp.GetShard())
	if err != nil {
		log.WarnContext(ctx, "failed to parse shard", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to parse shard")
		return nil, err
	}
	var rowSize int
	if len(result.proofs) > 0 && len(result.proofs[0].Row) > 0 {
		rowSize = len(result.proofs[0].Row)
	}
	span.AddEvent("rows_received", trace.WithAttributes(
		attribute.Int("row_count", len(result.proofs)),
		attribute.Int("row_size", rowSize),
	))

	if rlcVerifier != nil {
		if len(result.rlcCoeffs) == 0 || rowSize <= 0 || len(result.proofs) == 0 {
			return nil, fmt.Errorf("RLC verification requested but response is incomplete: coeffs=%d rowSize=%d proofs=%d",
				len(result.rlcCoeffs), rowSize, len(result.proofs))
		}
		rlcOrig, parseErr := parseRLCCoeffs(result.rlcCoeffs, rlcVerifier.cfg.OriginalRows)
		if parseErr != nil {
			log.WarnContext(ctx, "failed to parse RLC coefficients, skipping validator", "error", parseErr)
			return nil, fmt.Errorf("failed to parse RLC coefficients: %w", parseErr)
		} else if setErr := rlcVerifier.setOrWaitVerificationContext(rlcOrig, rowSize, &result.proofs[0].RowProof); setErr != nil {
			log.WarnContext(ctx, "RLC verification context rejected, skipping validator", "error", setErr)
			return nil, fmt.Errorf("failed to set RLC coefficients: %w", setErr)
		}
	}

	verified := make([]*rsema1d.RowInclusionProof, 0, len(result.proofs))
	for _, row := range result.proofs {
		var verifyErr error
		if rlcVerifier != nil {
			verifyErr = rlcVerifier.verifyRow(row)
		} else {
			verifyErr = blob.VerifyRow(row)
		}
		if verifyErr != nil {
			log.WarnContext(ctx, "invalid row", "row_index", row.Index, "error", verifyErr)
			span.AddEvent("invalid_row", trace.WithAttributes(
				attribute.Int("row_index", row.Index),
				attribute.String("error", verifyErr.Error()),
			))
			continue
		}
		verified = append(verified, row)
	}

	if len(verified) == 0 {
		log.WarnContext(ctx, "no valid rows from validator", "rows_total", len(result.proofs), "row_size", rowSize)
		span.SetStatus(codes.Error, "no valid rows")
		return nil, fmt.Errorf("no valid rows from validator %s", val.Address)
	}

	log.DebugContext(ctx, "got rows", "rows_total", len(result.proofs), "verified", len(verified), "row_size", rowSize)
	span.SetStatus(codes.Ok, "")
	return verified, nil
}

// downloadResult holds the result of a single validator shard download.
type downloadResult struct {
	valIdx int
	rows   []*rsema1d.RowInclusionProof
	err    error
}

// downloadBlob fans out shard fetches to all selected validators and populates
// the blob. Rows are applied single-threaded in the coordinator goroutine.
// Returns as soon as enough unique rows are collected; the deferred ctx cancel
// unwinds in-flight fetches to peers we no longer need.
// When withRLC is true, requests RLC coefficients for stronger per-row verification.
func (c *Client) downloadBlob(
	ctx context.Context,
	valSet validator.Set,
	id BlobID,
	verifyRLC bool,
) (*Blob, error) {
	span := trace.SpanFromContext(ctx)
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(errDownloaded)

	blob, err := NewEmptyBlob(id)
	if err != nil {
		return nil, fmt.Errorf("creating empty blob: %w", err)
	}

	blobCfg := blob.Config()
	originalRows := blobCfg.OriginalRows
	var rlcVerifier *rlcVerifier
	if verifyRLC {
		rlcVerifier = newRLCVerifier(id.Commitment(), blobCfg)
	}

	selected := valSet.Select(originalRows, c.Config.MinRowsPerValidator, c.Config.LivenessThreshold)
	resultCh := make(chan downloadResult, len(selected))

	for valIdx, sv := range selected {
		c.closeWg.Go(func() {
			rows, err := c.downloadFrom(ctx, sv.Validator, blob, id, rlcVerifier)
			select {
			case resultCh <- downloadResult{valIdx: valIdx, rows: rows, err: err}:
			case <-ctx.Done():
			}
		})
	}

	uniqueRows := 0
	for range selected {
		select {
		case res := <-resultCh:
			if res.err != nil {
				c.log.WarnContext(ctx, "shard fetch failed",
					"validator", selected[res.valIdx].Address,
					"error", res.err,
				)
				continue
			}

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

		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if uniqueRows >= originalRows {
			break
		}
	}

	switch {
	case uniqueRows == 0:
		return nil, ErrNotFound
	case uniqueRows < originalRows:
		return nil, ErrNotEnoughShards
	default:
		return blob, nil
	}
}

// errDownloaded signals that context was cancelled because download completed successfully.
var errDownloaded = errors.New("downloaded")

// shardResult holds parsed shard data including row proofs and optional RLC coefficients.
type shardResult struct {
	proofs    []*rsema1d.RowInclusionProof
	rlcCoeffs []byte // non-nil when coefficients are present (with_rlc download)
}

// parseShard extracts rows from the BlobShard response, constructing RowInclusionProofs.
// Also passes through RLC coefficients if present in the shard.
func parseShard(shard *types.BlobShard) (*shardResult, error) {
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

	return &shardResult{
		proofs:    proofs,
		rlcCoeffs: shard.GetCoefficients(),
	}, nil
}
