package fibre

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/celestiaorg/rsema1d"
	"github.com/celestiaorg/rsema1d/field"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	txsigning "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Upload uploads the given [Blob] to the Fibre network.
// It creates a [PaymentPromise], uploads the data to validators, and collects signatures confirming the upload.
// Returns a [SignedPaymentPromise] containing the promise and validator signatures.
// May keep uploading data in background after returning successfully.
// Returns [ErrClientClosed] if the client has been closed.
func (c *Client) Upload(ctx context.Context, ns share.Namespace, blob *Blob) (result SignedPaymentPromise, err error) {
	if c.closed.Load() {
		return result, ErrClientClosed
	}

	ctx, span := c.tracer.Start(ctx, "fibre.Client.Upload",
		trace.WithAttributes(
			attribute.String("namespace", ns.String()),
			attribute.Int("blob_size", blob.Size()),
		),
	)
	defer span.End()

	// 1) get validator set
	valSet, err := c.valGet.Head(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get validator set")
		return result, fmt.Errorf("fibre: getting validator set: %w", err)
	}
	span.AddEvent("validator_set", trace.WithAttributes(
		attribute.Int("validator_count", len(valSet.Validators)),
		attribute.Int64("validator_set_height", int64(valSet.Height)),
	))

	// 2) prepare payment promise
	promise, err := c.signedPromise(ns, blob, int64(valSet.Height))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create signed promise")
		return result, fmt.Errorf("fibre: %w", err)
	}
	promiseHash, err := promise.Hash()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to compute promise hash")
		return result, fmt.Errorf("fibre: computing promise hash: %w", err)
	}
	span.AddEvent("signed_promise", trace.WithAttributes(
		attribute.String("promise_hash", hex.EncodeToString(promiseHash)),
	))

	// 2) assign rows to validators
	shardMap := valSet.Assign(rsema1d.Commitment(blob.Commitment()), c.cfg.OriginalRows+c.cfg.ParityRows)
	span.AddEvent("rows_assigned")

	signBytes, err := promise.SignBytes()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to prepare bytes to sign")
		return result, fmt.Errorf("preparing bytes to sign: %w", err)
	}

	requests := makeUploadRequests(shardMap, promise.ToProto(), blob.RLCOrig())
	sigSet := valSet.NewSignatureSet(c.cfg.UploadTargetVotingPower, c.cfg.UploadTargetSignaturesCount, signBytes)

	c.log.DebugContext(ctx, "initiating blob upload",
		"promise_hash", hex.EncodeToString(promiseHash),
		"height", promise.Height,
		"namespace", ns.String(),
		"blob_size", promise.BlobSize,
		"commitment", promise.Commitment.String(),
		"validators", len(requests),
	)

	// 3) upload data
	if err = c.uploadAll(ctx, requests, blob, sigSet); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to upload")
		return result, err
	}

	// 5) collect signatures
	sigs, err := sigSet.Signatures()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to collect signatures")
		return result, err
	}

	c.log.DebugContext(ctx, "blob upload completed",
		"promise_hash", hex.EncodeToString(promiseHash),
		"commitment", promise.Commitment.String(),
		"signatures_collected", len(sigs),
	)

	span.SetStatus(codes.Ok, "")
	return SignedPaymentPromise{
		PaymentPromise:      promise,
		ValidatorSignatures: sigs,
	}, nil
}

// signerKey retrieves the secp256k1 public key from the keyring.
func (c *Client) signerKey() (*secp256k1.PubKey, error) {
	key, err := c.keyring.Key(c.cfg.DefaultKeyName)
	if err != nil {
		return nil, fmt.Errorf("getting key from keyring: %w", err)
	}

	pubKey, err := key.GetPubKey()
	if err != nil {
		return nil, fmt.Errorf("getting public key from keyring: %w", err)
	}

	cosmosPubKey, ok := pubKey.(*secp256k1.PubKey)
	if !ok {
		return nil, fmt.Errorf("expected secp256k1 public key, got %T", pubKey)
	}

	return cosmosPubKey, nil
}

// signedPromise creates and signs a [PaymentPromise].
func (c *Client) signedPromise(ns share.Namespace, blob *Blob, height int64) (*PaymentPromise, error) {
	signerKey, err := c.signerKey()
	if err != nil {
		return nil, err
	}

	promise := &PaymentPromise{
		ChainID:           c.cfg.ChainID,
		Height:            height,
		Namespace:         ns,
		BlobSize:          uint32(blob.Size()), // actual blob size with encoding overhead, rather then data size
		BlobVersion:       uint32(c.cfg.BlobVersion),
		Commitment:        blob.Commitment(),
		CreationTimestamp: c.clock.Now(),
		SignerKey:         signerKey,
	}

	signBytes, err := promise.SignBytes()
	if err != nil {
		return nil, fmt.Errorf("getting sign bytes: %w", err)
	}

	// sign using the default key and direct mode
	signature, _, err := c.keyring.Sign(c.cfg.DefaultKeyName, signBytes, txsigning.SignMode_SIGN_MODE_DIRECT)
	if err != nil {
		return nil, fmt.Errorf("signing payment promise: %w", err)
	}

	promise.Signature = signature
	return promise, nil
}

// uploadToValidator uploads rows to a single validator and adds the response signature to the signature set.
func (c *Client) uploadToValidator(
	ctx context.Context,
	val *core.Validator,
	req *types.UploadRowsRequest,
	blob *Blob,
	sigSet *validator.SignatureSet,
) {
	ctx, span := c.tracer.Start(ctx, "fibre.upload_to_validator",
		trace.WithAttributes(
			attribute.String("validator_address", val.Address.String()),
			attribute.Int("row_count", len(req.Rows)),
		),
	)
	defer span.End()

	// get a new or cached client with active connection
	client, err := c.clientCache.GetClient(ctx, val)
	if err != nil {
		c.log.WarnContext(ctx, "failed to get client for validator", "validator", val.Address.String(), "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get client")
		return
	}
	span.AddEvent("client_acquired")

	// get proofs and rows here in per request routine which is in parallel which ~39% faster for max blob size
	for i, rowPb := range req.Rows {
		row, err := blob.Row(int(rowPb.Index))
		if err != nil {
			c.log.WarnContext(ctx, "failed to generate proof for row", "validator", val.Address.String(), "row_index", rowPb.Index, "error", err)
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to generate proof")
			return
		}
		req.Rows[i].Data = row.Row
		req.Rows[i].Proof = row.RowProof
	}
	span.AddEvent("proofs_generated")

	// actually push the data to the validator
	resp, err := client.UploadRows(ctx, req)
	if err != nil {
		c.log.WarnContext(ctx, "failed to upload rows to validator", "validator", val.Address.String(), "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to upload rows")
		return
	}
	span.AddEvent("rows_uploaded")

	// apply signature response
	err = sigSet.Add(val, resp.ValidatorSignature)
	if err != nil {
		c.log.WarnContext(ctx, "failed to add signature from validator", "validator", val.Address.String(), "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to add signature")
		return
	}

	span.AddEvent("signature_verified")
	span.SetStatus(codes.Ok, "")
	c.log.DebugContext(ctx, "successfully uploaded to validator",
		"validator", val.Address.String(),
		"rows", len(req.Rows),
	)
}

// uploadAll pushes rows to all validators concurrently and collects signature responses.
// Returns when either all the responses are exhausted or signatures collected or the context is done.
// It continues uploading to every validator even after necessary amount of signatures are collected.
func (c *Client) uploadAll(
	ctx context.Context,
	requests map[*core.Validator]*types.UploadRowsRequest,
	blob *Blob,
	sigSet *validator.SignatureSet,
) error {
	var (
		responses            atomic.Uint32         // tracks finished responses
		responsesExhaustedCh = make(chan struct{}) // closes when all responses complete
	)

	for val, req := range requests {
		// acquire semaphore before spawning goroutine
		select {
		case c.uploadSem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}

		c.closeWg.Add(1)
		go func(val *core.Validator, req *types.UploadRowsRequest) {
			defer func() {
				// release semaphore
				<-c.uploadSem

				// increment responses and mark as completed if so
				if int(responses.Add(1)) == len(requests) {
					close(responsesExhaustedCh)
				}

				// unblock Close
				c.closeWg.Done()
			}()

			c.uploadToValidator(ctx, val, req, blob, sigSet)
		}(val, req)
	}

	select {
	case <-responsesExhaustedCh: // no more responses to wait for
	case <-sigSet.Done(): // enough signatures collected
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// makeUploadRequests constructs the requests map for all validators.
func makeUploadRequests(
	shardMap validator.ShardMap,
	pbPromise *types.PaymentPromise,
	rlcOrig []field.GF128,
) map[*core.Validator]*types.UploadRowsRequest {
	rlcOrigBytes := make([][]byte, len(rlcOrig))
	for i, coeff := range rlcOrig {
		b := field.ToBytes128(coeff)
		rlcOrigBytes[i] = b[:]
	}

	requests := make(map[*core.Validator]*types.UploadRowsRequest, len(shardMap))
	for val, rowIndices := range shardMap {
		req := &types.UploadRowsRequest{
			Promise: pbPromise,
			Rows:    make([]*types.Row, 0, len(rowIndices)),
			RlcOrig: rlcOrigBytes,
		}
		for _, rowIndex := range rowIndices {
			req.Rows = append(req.Rows, &types.Row{
				Index: uint32(rowIndex),
			})
		}
		requests[val] = req
	}
	return requests
}
