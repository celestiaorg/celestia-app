package fibre

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UploadShard handles the [types.FibreServer.UploadShard] RPC call.
func (s *Server) UploadShard(ctx context.Context, req *types.UploadShardRequest) (_ *types.UploadShardResponse, err error) {
	ctx, span := s.tracer.Start(ctx, "fibre.Server.UploadShard")
	defer span.End()

	var uploadSize int64
	uploadShardDone := s.metrics.observeUploadShard(ctx)
	defer func() { uploadShardDone(uploadSize, err) }()

	promise, blobCfg, promiseHash, pruneAt, err := s.verifyPromise(ctx, req.Promise)
	if err != nil {
		s.log.WarnContext(ctx, "payment promise verification failed", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "payment promise verification failed")
		return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("payment promise verification failed: %v", err))
	}

	uploadSize = int64(promise.UploadSize)
	log := s.log.With("blob_commitment", promise.Commitment.String(), "promise_height", promise.Height)

	span.AddEvent("promise_verified", trace.WithAttributes(
		attribute.String("promise_hash", hex.EncodeToString(promiseHash)),
		attribute.String("blob_commitment", promise.Commitment.String()),
		attribute.Int64("promise_height", int64(promise.Height)),
		attribute.String("namespace", promise.Namespace.String()),
		attribute.Int64("upload_size", int64(promise.UploadSize)),
	))

	// verify assignment - check that the shard belongs to us
	if err := s.verifyAssignment(ctx, promise, blobCfg, req.Shard); err != nil {
		log.WarnContext(ctx, "shard assignment verification failed", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "shard assignment verification failed")
		return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("shard assignment verification failed: %v", err))
	}
	span.AddEvent("assignment_verified")

	// verify row proofs using rsema1d and set RLC root
	if err := s.verifyShard(ctx, blobCfg, promise, req.Shard); err != nil {
		log.WarnContext(ctx, "shard verification failed", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "shard verification failed")
		return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("shard verification failed: %v", err))
	}
	span.AddEvent("shard_verified", trace.WithAttributes(
		attribute.Int("row_size", len(req.Shard.Rows[0].Data)), // this must be valid, as we just verified the rows, so no panics
		attribute.Int("rows_count", len(req.Shard.Rows)),
	))

	// store payment promise and shard with RLC roots
	storePutStart := time.Now()
	if err := s.store.Put(ctx, promise, req.Shard, pruneAt); err != nil {
		s.metrics.observeStoreOp(ctx, s.metrics.storePutDuration, storePutStart, false)
		log.ErrorContext(ctx, "failed to store upload data", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to store upload data")
		return nil, status.Error(grpccodes.Internal, fmt.Sprintf("failed to store upload data: %v", err))
	}
	s.metrics.observeStoreOp(ctx, s.metrics.storePutDuration, storePutStart, true)
	span.AddEvent("shard_stored")

	// sign the payment promise
	signStart := time.Now()
	signature, err := SignPaymentPromiseValidator(promise, s.signer)
	s.metrics.observeSign(ctx, signStart, err == nil)
	if err != nil {
		log.ErrorContext(ctx, "failed to sign payment promise", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to sign payment promise")
		return nil, status.Error(grpccodes.Internal, fmt.Sprintf("failed to sign payment promise: %v", err))
	}
	span.AddEvent("signature_generated")

	shardBytes := int64(len(req.Shard.Rows)) * int64(len(req.Shard.Rows[0].Data))
	s.metrics.uploadShardBytes.Add(ctx, shardBytes)
	log.DebugContext(ctx, "successful upload",
		"upload_size", promise.UploadSize,
		"shard_bytes", shardBytes,
		"rows_count", len(req.Shard.Rows),
		"row_size", len(req.Shard.Rows[0].Data),
	)

	span.SetStatus(codes.Ok, "")
	return &types.UploadShardResponse{
		ValidatorSignature: signature,
	}, nil
}

// verifyPromise verifies given proto of [PaymentPromise] and returns unmarshaled form with its hash.
// It does both stateless and stateful verification.
// Returns the BlobConfig for the blob version and pruneAt time based on the expiration time from chain state.
func (s *Server) verifyPromise(ctx context.Context, promisePb *types.PaymentPromise) (*PaymentPromise, BlobConfig, []byte, time.Time, error) {
	promise := &PaymentPromise{}
	if err := promise.FromProto(promisePb); err != nil {
		return nil, BlobConfig{}, nil, time.Time{}, fmt.Errorf("invalid payment promise proto: %w", err)
	}

	// validate PP chain ID matches the connected app chain ID
	chainID := s.state.ChainID()
	if promise.ChainID != chainID {
		return nil, BlobConfig{}, nil, time.Time{}, fmt.Errorf("payment promise chain ID mismatch: expected %s, got %s", chainID, promise.ChainID)
	}
	// validate blob version is supported
	blobCfg, err := BlobConfigForVersion(uint8(promise.BlobVersion))
	if err != nil {
		return nil, BlobConfig{}, nil, time.Time{}, fmt.Errorf("unsupported blob version %d: %w", promise.BlobVersion, err)
	}

	// stateless validation
	if err := promise.Validate(); err != nil {
		return nil, BlobConfig{}, nil, time.Time{}, fmt.Errorf("payment promise validation failed: %w", err)
	}

	// validate stateful constraints
	verifyResult, err := s.state.VerifyPromise(ctx, promisePb)
	if err != nil {
		return nil, BlobConfig{}, nil, time.Time{}, fmt.Errorf("stateful validation: %w", err)
	}

	promiseHash, err := promise.Hash()
	if err != nil {
		return nil, BlobConfig{}, nil, time.Time{}, fmt.Errorf("computing payment promise hash: %w", err)
	}

	return promise, blobCfg, promiseHash, verifyResult.ExpiresAt, nil
}

// verifyAssignment verifies that the [types.BlobShard] in the request is assigned to this validator.
// It fetches the validator set at the promise height, identifies this validator,
// computes the shard map, and checks that every row index belongs to this validator.
func (s *Server) verifyAssignment(ctx context.Context, promise *PaymentPromise, blobCfg BlobConfig, shard *types.BlobShard) error {
	valSet, err := s.state.GetByHeight(ctx, promise.Height)
	if err != nil {
		return fmt.Errorf("getting validator set at height %d: %w", promise.Height, err)
	}

	pubKey, err := s.signer.GetPubKey()
	if err != nil {
		return fmt.Errorf("getting validator public key: %w", err)
	}

	ourValidator, found := valSet.GetByAddress(pubKey.Address())
	if !found {
		return fmt.Errorf("validator %s not in set at height %d", pubKey.Address(), promise.Height)
	}

	// compute and verify assignment of rows in the request are assigned to us
	rowIndices := make([]uint32, len(shard.Rows))
	for i, row := range shard.GetRows() {
		rowIndices[i] = row.Index
	}
	shardMap := valSet.Assign(promise.Commitment, blobCfg.TotalRows(), blobCfg.OriginalRows, s.Config.MinRowsPerValidator, s.Config.LivenessThreshold)
	if err := shardMap.Verify(ourValidator, rowIndices); err != nil {
		return err
	}

	return nil
}

// verifyShard verifies the shard's rows and proofs using a pooled
// [rsema1d.Verifier], blocking until one is free. Sets the RLC root on the
// shard for inclusion-proof verification by non-RLC downloaders.
func (s *Server) verifyShard(ctx context.Context, blobCfg BlobConfig, promise *PaymentPromise, shard *types.BlobShard) error {
	rowSize, err := parseRowSize(shard.Rows)
	if err != nil {
		return err
	}

	// validate upload size matches the row size
	expectedUploadSize := rowSize * blobCfg.OriginalRows
	if int(promise.UploadSize) != expectedUploadSize {
		return fmt.Errorf("upload size mismatch: promise has %d, but row size %d * %d original rows = %d",
			promise.UploadSize, rowSize, blobCfg.OriginalRows, expectedUploadSize)
	}

	rlcCoeffs, err := parseRLCCoeffs(shard.GetCoefficients(), blobCfg.OriginalRows)
	if err != nil {
		return err
	}

	rows := make([]*rsema1d.RowProof, len(shard.Rows))
	for i, rowPb := range shard.Rows {
		row, err := parseRow(rowPb)
		if err != nil {
			return err
		}
		rows[i] = row
	}

	verifier, err := s.getVerifier(ctx)
	if err != nil {
		return fmt.Errorf("acquiring verifier: %w", err)
	}
	defer s.putVerifier(verifier)

	rlcRoot, err := verifier.Verify(promise.Commitment, rlcCoeffs, rows)
	if err != nil {
		return fmt.Errorf("shard row verification failed: %w", err)
	}

	// set RLC root, keep coefficients as-is for storage
	shard.Root = rlcRoot
	return nil
}

// newVerifierPool eagerly populates a buffered channel with n Verifiers
// for the v0 blob layout, each pinned to WorkerCount=1 (concurrency is
// the channel capacity).
func newVerifierPool(n int) chan *rsema1d.Verifier {
	blobCfg, err := BlobConfigForVersion(0)
	if err != nil {
		panic(fmt.Sprintf("v0 BlobConfig must exist: %v", err))
	}
	verifiers := make(chan *rsema1d.Verifier, n)
	for i := range n {
		v, err := rsema1d.NewVerifier(&rsema1d.Config{
			K:           blobCfg.OriginalRows,
			N:           blobCfg.ParityRows,
			RowSize:     0, // variable; resolved per Verify call from proof rows
			WorkerCount: 1,
		})
		if err != nil {
			panic(fmt.Sprintf("creating verifier %d: %v", i, err))
		}
		verifiers <- v
	}
	return verifiers
}

// getVerifier returns a Verifier from the pool, blocking until one is free
// or ctx is cancelled. Pair with putVerifier.
func (s *Server) getVerifier(ctx context.Context) (*rsema1d.Verifier, error) {
	select {
	case v := <-s.verifiers:
		return v, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// putVerifier returns a Verifier to the pool. The channel is sized to pool
// capacity, so this never blocks for any verifier previously obtained via
// getVerifier.
func (s *Server) putVerifier(v *rsema1d.Verifier) {
	if v == nil {
		return
	}
	s.verifiers <- v
}

// parseRLCCoeffs validates and converts RLC coefficients from bytes to field elements.
func parseRLCCoeffs(rlcCoeffs []byte, expectedCount int) ([]field.GF128, error) {
	expectedLen := expectedCount * 16
	if len(rlcCoeffs) != expectedLen {
		return nil, fmt.Errorf("expected %d bytes for %d rlc coefficients, got %d", expectedLen, expectedCount, len(rlcCoeffs))
	}

	coeffs := make([]field.GF128, expectedCount)
	for i := range expectedCount {
		var coeffArray [16]byte
		copy(coeffArray[:], rlcCoeffs[i*16:(i+1)*16])
		coeffs[i] = field.FromBytes128(coeffArray)
	}

	return coeffs, nil
}

// parseRowSize determines and validates the row size from all rows.
// Ensures that all rows have the same size.
func parseRowSize(rows []*types.BlobRow) (int, error) {
	if len(rows) == 0 {
		return 0, errors.New("no rows provided")
	}
	rowSize := len(rows[0].Data)
	if rowSize == 0 {
		return 0, errors.New("row size cannot be zero")
	}

	// validate all rows have the same size
	for i := 1; i < len(rows); i++ {
		if len(rows[i].Data) != rowSize {
			return 0, fmt.Errorf("row %d has size %d, expected %d (all rows must have the same size)", i, len(rows[i].Data), rowSize)
		}
	}

	return rowSize, nil
}

// parseRow validates and converts a single proto row to rsema1d.RowProof format.
func parseRow(row *types.BlobRow) (*rsema1d.RowProof, error) {
	if len(row.Proof) == 0 {
		return nil, fmt.Errorf("row %d missing proof", row.Index)
	}
	if len(row.Data) == 0 {
		return nil, fmt.Errorf("row %d missing data", row.Index)
	}

	return &rsema1d.RowProof{
		Index:    int(row.Index),
		Row:      row.Data,
		RowProof: row.Proof,
	}, nil
}
