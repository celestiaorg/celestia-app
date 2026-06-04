package fibre

import (
	"context"
	"errors"
	"fmt"
	"time"

	fibregrpc "github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
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

// Download retrieves and reconstructs a [Blob] by [BlobID] from the network.
//
// Validators are selected shuffled by stake weight and queried for shards in
// parallel until enough unique rows are collected to reconstruct the blob;
// failed requests automatically pull in more validators.
//
// The commitment binds the bytes, not their meaning. A malicious uploader can
// publish a self-consistent encoding over arbitrary data — every shard will
// verify, reconstruction will succeed, and the returned blob's content may not
// match what the caller expected. The protocol guarantees that every honest
// downloader of the same [BlobID] reconstructs byte-identical data; deciding
// whether that data is the "right" data is the caller's responsibility.
//
// Errors:
//   - [ErrNotFound]: no shards were retrieved
//   - [ErrNotEnoughShards]: not enough shards to reconstruct
//   - reconstruction or decoding errors if the rows cannot produce a valid blob
func (c *Client) Download(ctx context.Context, id BlobID, opts ...DownloadOption) (blob *Blob, err error) {
	if !c.started.Load() {
		return nil, errors.New("fibre client is not started")
	}
	if c.closed.Load() {
		return nil, ErrClientClosed
	}
	if err := id.Validate(); err != nil {
		return nil, fmt.Errorf("invalid blob ID: %w", err)
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

	// Prefer the exact validator set at height when provided; otherwise fall
	// back to the head set — stakes are stable enough that this only affects
	// the number of validators contacted, not correctness.
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

	blobCfg, err := BlobConfigForVersion(id.Version())
	if err != nil {
		return nil, err
	}

	blob, err = c.downloadBlob(ctx, valSet, id, blobCfg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to download")
		return nil, err
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

// downloadFrom runs one validator's full attempt: fetches the shard, hands it
// to the download, and emits per-attempt tracing/metrics. A non-nil return
// leaves the reservation held — the caller must [download.SkipShard]. A nil
// return means [download.AddShard] committed the shard and consumed the
// reservation.
func (c *Client) downloadFrom(
	ctx context.Context,
	from validator.SelectedValidator,
	id BlobID,
	state *download,
) (err error) {
	log := c.log.With("validator", from.Address.String(), "blob_commitment", id.Commitment())

	downloadStart := time.Now()
	valAddrStr := from.Address.String()

	ctx, span := c.tracer.Start(ctx, "download_from",
		trace.WithAttributes(
			attribute.String("validator_address", valAddrStr),
			attribute.Int("expected_rows", from.ExpectedRows),
		),
	)
	defer span.End()

	defer func() {
		success := err == nil || context.Cause(ctx) == errDownloaded
		c.metrics.observeDownloadFrom(ctx, downloadStart, success, valAddrStr)
	}()

	var resp *types.DownloadShardResponse
	rpcStart := time.Now()
	err = c.clientCache.Request(ctx, from.Validator, func(client fibregrpc.Client) error {
		rpcCtx, rpcCancel := context.WithTimeout(ctx, c.Config.RPCTimeout)
		defer rpcCancel()
		var err error
		resp, err = client.DownloadShard(rpcCtx, &types.DownloadShardRequest{BlobId: id})
		return err
	})
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

	proofs, rlc, err := parseShard(resp.GetShard())
	if err != nil {
		log.WarnContext(ctx, "failed to parse shard", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to parse shard")
		return err
	}

	rowSize := 0
	if len(proofs) > 0 {
		rowSize = len(proofs[0].Row)
	}
	span.AddEvent("rows_received", trace.WithAttributes(
		attribute.Int("row_count", len(proofs)),
		attribute.Int("row_size", rowSize),
	))
	log.DebugContext(ctx, "got shard", "rows_total", len(proofs), "row_size", rowSize)

	if len(proofs) < from.ExpectedRows {
		log.WarnContext(ctx, "validator under-delivered shard",
			"got", len(proofs), "expected", from.ExpectedRows)
	}

	if err := state.AddShard(from, proofs, rlc); err != nil {
		log.WarnContext(ctx, "invalid shard", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid shard")
		return err
	}
	span.AddEvent("rows_added", trace.WithAttributes(
		attribute.Int("total_rows", state.RowsCount()),
	))
	span.SetStatus(codes.Ok, "")
	return nil
}

// downloadBlob downloads shards and reconstructs the K original rows behind
// id, returning them wrapped in a [Blob] that aliases the underlying pool
// slab. Callers must invoke [Blob.Free] to release the slab.
func (c *Client) downloadBlob(
	ctx context.Context,
	valSet validator.Set,
	id BlobID,
	blobCfg BlobConfig,
) (*Blob, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(errDownloaded)

	selected := valSet.Select(blobCfg.OriginalRows, c.Config.MinRowsPerValidator, c.Config.LivenessThreshold)
	state, err := newDownload(blobCfg, id, selected)
	if err != nil {
		return nil, err
	}

	for from := range state.ShardSources(ctx) {
		c.closeWg.Go(func() {
			if err := c.downloadFrom(ctx, from, id, state); err != nil {
				state.SkipShard(from)
			}
		})
	}

	return state.Blob(ctx)
}

// errDownloaded signals that context was cancelled because download completed successfully.
var errDownloaded = errors.New("downloaded")

// parseShard extracts row proofs and the parsed RLC vector from a BlobShard
// response.
func parseShard(shard *types.BlobShard) ([]*rsema1d.RowProof, rlc.Vector, error) {
	if shard == nil {
		return nil, nil, fmt.Errorf("shard response is nil")
	}

	rowsArray := shard.GetRows()
	if len(rowsArray) == 0 {
		return nil, nil, fmt.Errorf("no rows in shard")
	}

	proofs := make([]*rsema1d.RowProof, len(rowsArray))
	for i, row := range rowsArray {
		if row == nil {
			return nil, nil, fmt.Errorf("shard row %d is nil", i)
		}
		proofs[i] = &rsema1d.RowProof{
			Index:    int(row.Index),
			Row:      row.Data,
			RowProof: row.Proof,
		}
	}

	rlcs, err := rlc.Unmarshal(shard.GetRlcs())
	if err != nil {
		return nil, nil, err
	}
	if len(rlcs) == 0 {
		return nil, nil, errors.New("validator returned no RLCs")
	}

	return proofs, rlcs, nil
}
