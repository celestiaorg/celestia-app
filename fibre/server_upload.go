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

// UploadRows handles the [types.FibreServer.UploadRows] RPC call.
// It validates the [PaymentPromise], verifies row proofs, checks assignment, stores the data, and returns a signature.
func (s *Server) UploadRows(ctx context.Context, req *types.UploadRowsRequest) (*types.UploadRowsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "fibre.Server.UploadRows")
	defer span.End()

	promise, promiseHash, err := s.verifyPromise(ctx, req.Promise)
	if err != nil {
		s.log.WarnContext(ctx, "payment promise verification failed", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "payment promise verification failed")
		return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("payment promise verification failed: %v", err))
	}

	log := s.log.With("blob_commitment", promise.Commitment.String(), "promise_height", promise.Height)

	span.AddEvent("promise_validated", trace.WithAttributes(
		attribute.String("promise_hash", hex.EncodeToString(promiseHash)),
		attribute.String("blob_commitment", promise.Commitment.String()),
		attribute.Int64("promise_height", int64(promise.Height)),
		attribute.String("namespace", promise.Namespace.String()),
		attribute.Int64("upload_size", int64(promise.UploadSize)),
	))

	// verify assignment - check that all rows belong to us
	if err := s.verifyAssignment(ctx, promise, req.Rows); err != nil {
		log.WarnContext(ctx, "row assignment verification failed", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "row assignment verification failed")
		return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("row assignment verification failed: %v", err))
	}
	span.AddEvent("assignment_verified")

	// verify row proofs using rsema1d and set RLC root
	if err := s.verifyRows(ctx, promise, req.Rows); err != nil {
		log.WarnContext(ctx, "row verification failed", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "row verification failed")
		return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("row verification failed: %v", err))
	}
	span.AddEvent("rows_verified", trace.WithAttributes(
		attribute.Int("row_size", len(req.Rows.Rows[0].Data)), // this must be valid, as we just verified the rows, so no panics
		attribute.Int("row_count", len(req.Rows.Rows)),
	))

	// store payment promise and rows with RLC root
	if err := s.store.Put(ctx, promise, req.Rows); err != nil {
		log.ErrorContext(ctx, "failed to store upload data", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to store upload data")
		return nil, status.Error(grpccodes.Internal, fmt.Sprintf("failed to store upload data: %v", err))
	}
	span.AddEvent("data_stored")

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
		"rows", len(req.Rows.Rows),
		"row_size", len(req.Rows.Rows[0].Data),
	)

	span.SetStatus(codes.Ok, "")
	return &types.UploadRowsResponse{
		ValidatorSignature: signature,
	}, nil
}

// verifyPromise verifies given proto of [PaymentPromise] and returns unmarshaled form with its hash.
// It does both stateless and stateful verification.
func (s *Server) verifyPromise(ctx context.Context, promisePb *types.PaymentPromise) (*PaymentPromise, []byte, error) {
	promise := &PaymentPromise{}
	if err := promise.FromProto(promisePb); err != nil {
		return nil, nil, fmt.Errorf("invalid payment promise proto: %w", err)
	}

	// validate PP fields matches the config
	if promise.ChainID != s.cfg.ChainID {
		return nil, nil, fmt.Errorf("payment promise chain ID mismatch: expected %s, got %s", s.cfg.ChainID, promise.ChainID)
	}
	if promise.BlobVersion != uint32(s.cfg.BlobVersion) {
		return nil, nil, fmt.Errorf("blob version mismatch: expected %d, got %d", s.cfg.BlobVersion, promise.BlobVersion)
	}
	oldestAllowed := time.Now().UTC().Add(-s.cfg.PaymentPromiseTimeout)
	if promise.CreationTimestamp.Before(oldestAllowed) {
		return nil, nil, fmt.Errorf("payment promise expired: %s is before %s (timeout: %s)",
			promise.CreationTimestamp.Format(time.RFC3339),
			oldestAllowed.Format(time.RFC3339),
			s.cfg.PaymentPromiseTimeout)
	}
	// use height of the latest valset to verify height in the promise
	currentValSet, err := s.valGet.Head(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("getting current validator set: %w", err)
	}
	// calculate max height drift based on promise timeout and block time
	maxHeightDrift := uint64(s.cfg.PaymentPromiseTimeout / s.cfg.BlockTime)
	if currentValSet.Height > maxHeightDrift && promise.Height < currentValSet.Height-maxHeightDrift {
		return nil, nil, fmt.Errorf("payment promise height too far in past: %d is before min allowed %d (current: %d, timeout: %s, block time: %s)",
			promise.Height, currentValSet.Height-maxHeightDrift, currentValSet.Height, s.cfg.PaymentPromiseTimeout, s.cfg.BlockTime)
	}

	// stateless validation
	if err := promise.Validate(); err != nil {
		return nil, nil, fmt.Errorf("payment promise validation failed: %w", err)
	}

	// validate stateful constraints
	resp, err := s.queryClient.ValidatePaymentPromise(ctx, &types.QueryValidatePaymentPromiseRequest{
		Promise: *promisePb,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("stateful validation request: %w", err)
	}
	if !resp.IsValid {
		return nil, nil, fmt.Errorf("payment promise is invalid with no reason")
	}

	promiseHash, err := promise.Hash()
	if err != nil {
		return nil, nil, fmt.Errorf("computing payment promise hash: %w", err)
	}
	return promise, promiseHash, nil
}

// verifyAssignment verifies that all rows in the request are assigned to this validator.
// It fetches the validator set at the promise height, identifies this validator,
// computes the shard map, and checks that every row index belongs to this validator.
func (s *Server) verifyAssignment(ctx context.Context, promise *PaymentPromise, rows *types.Rows) error {
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
	rowIndices := make([]uint32, len(rows.Rows))
	for i, row := range rows.GetRows() {
		rowIndices[i] = row.Index
	}
	shardMap := valSet.Assign(rsema1d.Commitment(promise.Commitment), s.cfg.OriginalRows+s.cfg.ParityRows)
	if err := shardMap.Verify(ourValidator, rowIndices); err != nil {
		return err
	}

	return nil
}

// verifyRows verifies the row data and proofs using [rsema1d.VerificationContext].
// Essentially checks correctness of blob data by only sampling some of the rows.
// Sets the RLC root on the rows and clears the coefficients after verification.
func (s *Server) verifyRows(_ context.Context, promise *PaymentPromise, rows *types.Rows) error {
	rowSize, err := parseRowSize(rows.Rows)
	if err != nil {
		return err
	}

	// validate upload size matches the row size
	expectedUploadSize := rowSize * s.cfg.OriginalRows
	if int(promise.UploadSize) != expectedUploadSize {
		return fmt.Errorf("upload size mismatch: promise has %d, but row size %d * %d original rows = %d",
			promise.UploadSize, rowSize, s.cfg.OriginalRows, expectedUploadSize)
	}

	rlcCoeffs, err := parseRLCCoeffs(rows.GetCoefficients(), s.cfg.OriginalRows)
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
	for _, rowPb := range rows.Rows {
		row, err := parseRow(rowPb, totalRows)
		if err != nil {
			return err
		}

		if err := rsema1d.VerifyRowWithContext(row, rsema1d.Commitment(promise.Commitment), verificationCtx); err != nil {
			return fmt.Errorf("verification failed for row %d: %w", row.Index, err)
		}
	}

	// set RLC root and clear coefficients
	rows.Rlc = &types.Rows_Root{Root: rlcRoot[:]}
	return nil
}

// signPromise signs the [PaymentPromise] using the validator's private key and returns the signature.
func (s *Server) signPromise(promise *PaymentPromise) ([]byte, error) {
	signBytes, err := promise.SignBytes()
	if err != nil {
		return nil, fmt.Errorf("getting sign bytes: %w", err)
	}

	// sign using validator's private key
	signature, err := s.privVal.SignRawBytes(s.cfg.ChainID, "", signBytes) // signBytes already include domain separator, so we don't have to pass its
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
func parseRowSize(rows []*types.Row) (int, error) {
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
func parseRow(row *types.Row, totalRows int) (*rsema1d.RowProof, error) {
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
