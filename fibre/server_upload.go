package fibre

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	"github.com/celestiaorg/rsema1d"
	"github.com/celestiaorg/rsema1d/field"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UploadShard handles the [types.FibreServer.UploadShard] RPC call.
func (s *Server) UploadShard(ctx context.Context, req *types.UploadShardRequest) (*types.UploadShardResponse, error) {
	ctx, span := s.tracer.Start(ctx, "fibre.Server.UploadShard")
	defer span.End()

	promise, promiseHash, pruneAt, err := s.verifyPromise(ctx, req.Promise)
	if err != nil {
		s.log.WarnContext(ctx, "payment promise verification failed", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "payment promise verification failed")
		return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("payment promise verification failed: %v", err))
	}

	log := s.log.With("blob_commitment", promise.Commitment.String(), "promise_height", promise.Height)

	span.AddEvent("promise_verified", trace.WithAttributes(
		attribute.String("promise_hash", hex.EncodeToString(promiseHash)),
		attribute.String("blob_commitment", promise.Commitment.String()),
		attribute.Int64("promise_height", int64(promise.Height)),
		attribute.String("namespace", promise.Namespace.String()),
		attribute.Int64("upload_size", int64(promise.UploadSize)),
	))

	// verify assignment - check that the shard belongs to us
	if err := s.verifyAssignment(ctx, promise, req.Shard); err != nil {
		log.WarnContext(ctx, "shard assignment verification failed", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "shard assignment verification failed")
		return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("shard assignment verification failed: %v", err))
	}
	span.AddEvent("assignment_verified")

	// verify row proofs using rsema1d and set RLC root
	if err := s.verifyShard(ctx, promise, req.Shard); err != nil {
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
	if err := s.store.Put(ctx, promise, req.Shard, pruneAt); err != nil {
		log.ErrorContext(ctx, "failed to store upload data", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to store upload data")
		return nil, status.Error(grpccodes.Internal, fmt.Sprintf("failed to store upload data: %v", err))
	}
	span.AddEvent("shard_stored")

	// sign the payment promise
	signature, err := s.signPromise(promise)
	if err != nil {
		log.ErrorContext(ctx, "failed to sign payment promise", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to sign payment promise")
		return nil, status.Error(grpccodes.Internal, fmt.Sprintf("failed to sign payment promise: %v", err))
	}
	span.AddEvent("signature_generated")

	log.InfoContext(ctx, "successful upload",
		"upload_size", promise.UploadSize,
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
// Returns the pruneAt time for the shard based on the expiration time from chain state.
func (s *Server) verifyPromise(ctx context.Context, promisePb *types.PaymentPromise) (*PaymentPromise, []byte, time.Time, error) {
	promise := &PaymentPromise{}
	if err := promise.FromProto(promisePb); err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("invalid payment promise proto: %w", err)
	}

	// validate PP fields matches the config
	if promise.ChainID != s.cfg.ChainID {
		return nil, nil, time.Time{}, fmt.Errorf("payment promise chain ID mismatch: expected %s, got %s", s.cfg.ChainID, promise.ChainID)
	}
	if promise.BlobVersion != uint32(s.cfg.BlobVersion) {
		return nil, nil, time.Time{}, fmt.Errorf("blob version mismatch: expected %d, got %d", s.cfg.BlobVersion, promise.BlobVersion)
	}

	// stateless validation
	if err := promise.Validate(); err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("payment promise validation failed: %w", err)
	}

	// validate stateful constraints
	resp, err := s.queryClient.ValidatePaymentPromise(ctx, &types.QueryValidatePaymentPromiseRequest{
		Promise: *promisePb,
	})
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("stateful validation request: %w", err)
	}
	if !resp.IsValid {
		return nil, nil, time.Time{}, fmt.Errorf("payment promise is invalid with no reason")
	}
	if resp.ExpirationTime == nil {
		return nil, nil, time.Time{}, fmt.Errorf("expiration time not provided in validation response")
	}
	pruneAt := *resp.ExpirationTime

	promiseHash, err := promise.Hash()
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("computing payment promise hash: %w", err)
	}

	return promise, promiseHash, pruneAt, nil
}

// verifyAssignment verifies that the [types.BlobShard] in the request is assigned to this validator.
// It fetches the validator set at the promise height, identifies this validator,
// computes the shard map, and checks that every row index belongs to this validator.
func (s *Server) verifyAssignment(ctx context.Context, promise *PaymentPromise, shard *types.BlobShard) error {
	valSet, err := s.valGet.GetByHeight(ctx, promise.Height)
	if err != nil {
		return fmt.Errorf("getting validator set at height %d: %w", promise.Height, err)
	}

	// get our validator using the cached public key
	ourValidator, found := valSet.GetByAddress(s.pubKey.Address())
	if !found {
		return fmt.Errorf("validator %s not in set at height %d", s.pubKey.Address().String(), promise.Height)
	}

	// compute and verify assignment of rows in the request are assigned to us
	rowIndices := make([]uint32, len(shard.Rows))
	for i, row := range shard.GetRows() {
		rowIndices[i] = row.Index
	}
	shardMap := valSet.Assign(rsema1d.Commitment(promise.Commitment), s.cfg.OriginalRows+s.cfg.ParityRows)
	if err := shardMap.Verify(ourValidator, rowIndices); err != nil {
		return err
	}

	return nil
}

// verifyShard verifies [types.BlobShard]'s rows and proofs using [rsema1d.VerificationContext].
// Essentially checks correctness of the entire [Blob]'s data by only sampling subset of data rows.
// Sets the RLC root on the shard and clears the coefficients after verification.
func (s *Server) verifyShard(_ context.Context, promise *PaymentPromise, shard *types.BlobShard) error {
	rowSize, err := parseRowSize(shard.Rows)
	if err != nil {
		return err
	}

	// validate upload size matches the row size
	expectedUploadSize := rowSize * s.cfg.OriginalRows
	if int(promise.UploadSize) != expectedUploadSize {
		return fmt.Errorf("upload size mismatch: promise has %d, but row size %d * %d original rows = %d",
			promise.UploadSize, rowSize, s.cfg.OriginalRows, expectedUploadSize)
	}

	rlcCoeffs, err := parseRLCCoeffs(shard.GetCoefficients(), s.cfg.OriginalRows)
	if err != nil {
		return err
	}

	verificationCtx, rlcRoot, err := rsema1d.CreateVerificationContext(rlcCoeffs, &rsema1d.Config{
		K:           s.cfg.OriginalRows,
		N:           s.cfg.ParityRows,
		RowSize:     rowSize,
		WorkerCount: s.cfg.CodingWorkers,
	})
	if err != nil {
		return fmt.Errorf("creating verification context: %w", err)
	}

	totalRows := s.cfg.OriginalRows + s.cfg.ParityRows
	for _, rowPb := range shard.Rows {
		row, err := parseRow(rowPb, totalRows)
		if err != nil {
			return err
		}

		if err := rsema1d.VerifyRowWithContext(row, rsema1d.Commitment(promise.Commitment), verificationCtx); err != nil {
			return fmt.Errorf("verification failed for row %d: %w", row.Index, err)
		}
	}

	// set RLC root and clear coefficients
	shard.Rlc = &types.BlobShard_Root{Root: rlcRoot[:]}
	return nil
}

// signPromise signs the [PaymentPromise] using the validator's private key and returns the signature.
func (s *Server) signPromise(promise *PaymentPromise) ([]byte, error) {
	signBytes, err := promise.SignBytes()
	if err != nil {
		return nil, fmt.Errorf("getting sign bytes: %w", err)
	}

	// sign using validator's private key
	signature, err := s.privVal.SignRawBytes(s.cfg.ChainID, SignBytesPrefix, signBytes)
	if err != nil {
		return nil, fmt.Errorf("signing payment promise: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("invalid signature length: expected %d, got %d", ed25519.SignatureSize, len(signature))
	}

	return signature, nil
}

// parseRLCCoeffs validates and converts RLC coefficients from bytes to field elements.
func parseRLCCoeffs(rlcCoeffs []byte, expectedCount int) ([]field.GF128, error) {
	expectedLen := expectedCount * 16
	if len(rlcCoeffs) != expectedLen {
		return nil, fmt.Errorf("expected %d bytes for %d rlc coefficients, got %d", expectedLen, expectedCount, len(rlcCoeffs))
	}

	coeffs := make([]field.GF128, expectedCount)
	for i := 0; i < expectedCount; i++ {
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
func parseRow(row *types.BlobRow, totalRows int) (*rsema1d.RowProof, error) {
	if int(row.Index) >= totalRows {
		return nil, fmt.Errorf("row index %d out of bounds (total rows: %d)", row.Index, totalRows)
	}
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
