package fibre

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
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
			attribute.Int("upload_size", blob.UploadSize()),
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
	promise, err := c.signedPromise(ns, blob, valSet.Height)
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

	// 2) assign shards to validators
	blobCfg := blob.Config()
	shardMap := valSet.Assign(rsema1d.Commitment(blob.Commitment()), blobCfg.OriginalRows+blobCfg.ParityRows)
	span.AddEvent("shards_assigned")

	validatorSignBytes, err := promise.SignBytesValidator()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to prepare validator sign bytes")
		return result, fmt.Errorf("preparing validator sign bytes: %w", err)
	}

	requests := makeUploadRequests(shardMap, promise.ToProto(), blob.RLCCoeffs())
	sigSet := valSet.NewSignatureSet(c.cfg.UploadTargetVotingPower, c.cfg.UploadTargetSignaturesCount, validatorSignBytes)

	c.log.DebugContext(ctx, "initiating blob upload",
		"promise_hash", hex.EncodeToString(promiseHash),
		"promise_height", promise.Height,
		"namespace", ns.String(),
		"upload_size", promise.UploadSize,
		"blob_commitment", promise.Commitment.String(),
		"validators", len(requests),
	)

	// 3) upload data
	if err = c.uploadShards(ctx, requests, blob, sigSet); err != nil {
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
		"blob_commitment", promise.Commitment.String(),
		"upload_size", promise.UploadSize,
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
func (c *Client) signedPromise(ns share.Namespace, blob *Blob, height uint64) (*PaymentPromise, error) {
	signerKey, err := c.signerKey()
	if err != nil {
		return nil, err
	}

	promise := &PaymentPromise{
		ChainID:           c.cfg.ChainID,
		Height:            height,
		Namespace:         ns,
		UploadSize:        uint32(blob.UploadSize()),
		BlobVersion:       uint32(blob.Config().BlobVersion),
		Commitment:        blob.Commitment(),
		CreationTimestamp: c.clock.Now().UTC(),
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

// uploadTo uploads blob shard to a single validator and adds the response signature to the signature set.
// Returns true if enough signatures have been collected after adding this signature.
func (c *Client) uploadTo(
	ctx context.Context,
	val *core.Validator,
	req *types.UploadShardRequest,
	blob *Blob,
	sigSet *validator.SignatureSet,
) bool {
	ctx, cancel := context.WithCancel(ctx) // GRPC calls require context cancelling upon completion
	defer cancel()

	log := c.log.With(
		"validator", val.Address.String(),
		"blob_commitment", blob.Commitment(),
		"rows_count", len(req.Shard.Rows),
	)

	ctx, span := c.tracer.Start(ctx, "upload_to",
		trace.WithAttributes(
			attribute.String("validator_address", val.Address.String()),
			attribute.Int("rows_count", len(req.Shard.Rows)),
		),
	)
	defer span.End()

	// get a new or cached client with active connection
	client, err := c.clientCache.GetClient(ctx, val)
	if err != nil {
		log.WarnContext(ctx, "can't get grpc.FibreClient", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "can't get grpc.FibreClient")
		return false
	}
	span.AddEvent("client_acquired")

	// get proofs and rows here in per request routine which is in parallel which ~39% faster for max blob size
	for i, rowPb := range req.Shard.Rows {
		row, err := blob.Row(int(rowPb.Index))
		if err != nil {
			log.WarnContext(ctx, "failed to generate proof for row", "row_index", rowPb.Index, "error", err)
			span.RecordError(err, trace.WithAttributes(attribute.Int("row_index", int(rowPb.Index))))
			span.SetStatus(codes.Error, "failed to generate proof for row")
			return false
		}
		req.Shard.Rows[i].Data = row.Row
		req.Shard.Rows[i].Proof = row.RowProof.RowProof
	}
	span.AddEvent("proofs_generated")

	// actually push the data to the validator
	resp, err := client.UploadShard(ctx, req)
	if err != nil {
		log.WarnContext(ctx, "failed to upload rows", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to upload rows")
		return false
	}
	span.AddEvent("rows_uploaded")

	// validate and get signature
	signature, err := parseSignature(resp.ValidatorSignature)
	if err != nil {
		log.WarnContext(ctx, "failed to parse signature", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to parse signature")
		return false
	}

	// apply signature response and check if we have enough
	hasEnough, err := sigSet.Add(val, signature)
	if err != nil {
		log.WarnContext(ctx, "failed to add signature", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to add signature")
		return false
	}

	log.DebugContext(ctx, "successfully uploaded to validator")
	span.AddEvent("signature_verified")
	span.SetStatus(codes.Ok, "")
	return hasEnough
}

// uploadShards pushes assigned [types.BlobShard]s to all validators concurrently and collects signature responses.
// Returns when either all the responses are exhausted or signatures collected or the context is done.
// It continues uploading to every validator even after necessary amount of signatures is reached.
func (c *Client) uploadShards(
	ctx context.Context,
	requests map[*core.Validator]*types.UploadShardRequest,
	blob *Blob,
	sigSet *validator.SignatureSet,
) error {
	var (
		responses            atomic.Uint32         // tracks finished responses
		responsesExhaustedCh = make(chan struct{}) // closes when all responses complete
	)

	var (
		sigsCollectedOnce atomic.Bool
		sigsCollectedCh   = make(chan struct{}) // closes when enough signatures are collected
	)

	for val, req := range requests {
		// acquire semaphore before spawning goroutine
		select {
		case c.uploadSem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}

		c.closeWg.Add(1)
		go func(val *core.Validator, req *types.UploadShardRequest) {
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

			isDone := c.uploadTo(ctx, val, req, blob, sigSet)
			if isDone && sigsCollectedOnce.CompareAndSwap(false, true) {
				close(sigsCollectedCh)
			}
		}(val, req)
	}

	select {
	case <-responsesExhaustedCh: // no more responses to wait for
	case <-sigsCollectedCh: // enough signatures collected
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// makeUploadRequests constructs the requests map for all validators.
func makeUploadRequests(
	shardMap validator.ShardMap,
	pbPromise *types.PaymentPromise,
	rlcCoeffs []field.GF128,
) map[*core.Validator]*types.UploadShardRequest {
	// flatten rlc coefficients into a single byte slice (16 bytes per coefficient)
	rlcCoeffsBytes := make([]byte, len(rlcCoeffs)*16)
	for i, coeff := range rlcCoeffs {
		b := field.ToBytes128(coeff)
		copy(rlcCoeffsBytes[i*16:(i+1)*16], b[:])
	}

	requests := make(map[*core.Validator]*types.UploadShardRequest, len(shardMap))
	for val, rowIndices := range shardMap {
		rows := make([]*types.BlobRow, 0, len(rowIndices))
		for _, rowIndex := range rowIndices {
			rows = append(rows, &types.BlobRow{
				Index: uint32(rowIndex),
			})
		}
		req := &types.UploadShardRequest{
			Promise: pbPromise,
			Shard: &types.BlobShard{
				Rows: rows,
				Rlc:  &types.BlobShard_Coefficients{Coefficients: rlcCoeffsBytes},
			},
		}
		requests[val] = req
	}
	return requests
}

// parseSignature validates and returns the validator signature from the response.
func parseSignature(signature []byte) ([]byte, error) {
	if len(signature) == 0 {
		return nil, fmt.Errorf("validator signature is empty")
	}
	return signature, nil
}
