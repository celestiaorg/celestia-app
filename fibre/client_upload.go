package fibre

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	fibregrpc "github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	txsigning "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// UploadOption configures the behavior of [Client.Upload].
type UploadOption func(*uploadOptions)

type uploadOptions struct {
	keyName  string
	awaitAll bool
}

// WithKeyName sets the key name used for signing the payment promise.
// When not provided, the default key name from [ClientConfig] is used.
func WithKeyName(keyName string) UploadOption {
	return func(o *uploadOptions) {
		o.keyName = keyName
	}
}

// WithAwaitAllSignatures makes [Client.Upload] wait for all validators to respond
// instead of returning as soon as the safety threshold (2/3) of signatures is collected.
func WithAwaitAllSignatures() UploadOption {
	return func(o *uploadOptions) {
		o.awaitAll = true
	}
}

// Upload uploads the given [Blob] to the Fibre network.
// It creates a [PaymentPromise], uploads the data to validators, and collects signatures confirming the upload.
// Returns a [SignedPaymentPromise] containing the promise and validator signatures.
// May keep uploading data in background after returning successfully; use [Client.Await]
// or [Client.Stop] to drain.
//
// Canceling Context right after Upload drops remaining background uploads.
// Avoid immediate cancels if uploads redundancy matters (it usually does).
//
// The blob must not be reused after calling [Blob.Free].
// Returns [ErrClientClosed] if the client has been closed.
func (c *Client) Upload(ctx context.Context, ns share.Namespace, blob *Blob, opts ...UploadOption) (result SignedPaymentPromise, err error) {
	if !c.started.Load() {
		return result, errors.New("fibre: client is not started")
	}
	if c.closed.Load() {
		return result, ErrClientClosed
	}
	if !blob.retain() {
		return result, errors.New("fibre: blob already released; create a new blob to upload")
	}
	defer blob.release()

	opt := uploadOptions{keyName: c.Config.DefaultKeyName}
	for _, o := range opts {
		o(&opt)
	}

	ctx, span := c.tracer.Start(ctx, "fibre.Client.Upload",
		trace.WithAttributes(
			attribute.String("namespace", ns.String()),
			attribute.Int("upload_size", blob.UploadSize()),
		),
	)
	defer span.End()

	uploadDone := c.metrics.observeUpload(ctx, blob.UploadSize())
	defer func() { uploadDone(err) }()

	// 1) get validator set
	valSet, err := c.state.Head(ctx)
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
	promise, err := c.signedPromise(ns, blob, valSet.Height, opt.keyName)
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
	shardMap := valSet.Assign(blob.ID().Commitment(), blob.Config().TotalRows(), blob.Config().OriginalRows, c.Config.MinRowsPerValidator, c.Config.LivenessThreshold)
	span.AddEvent("shards_assigned")

	signBytes, err := promise.SignBytes()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to prepare validator sign bytes")
		return result, fmt.Errorf("preparing validator sign bytes: %w", err)
	}

	promiseProto, err := promise.ToProto()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to convert payment promise to proto")
		return result, fmt.Errorf("converting payment promise to proto: %w", err)
	}
	requests := makeUploadRequests(shardMap, promiseProto, blob.RLC())
	threshold := c.Config.SafetyThreshold
	if opt.awaitAll {
		threshold = cmtmath.Fraction{Numerator: 1, Denominator: 1}
	}
	sigSet := valSet.NewSignatureSet(threshold, signBytes)

	c.log.DebugContext(ctx, "initiating blob upload",
		"promise_hash", hex.EncodeToString(promiseHash),
		"promise_height", promise.Height,
		"namespace", ns.String(),
		"upload_size", promise.UploadSize,
		"blob_commitment", promise.Commitment.String(),
		"validators", len(requests),
	)

	// 3) upload data
	if err = c.uploadShards(ctx, shardMap, requests, blob, sigSet); err != nil {
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

	var totalShardRows int
	for _, rows := range shardMap {
		totalShardRows += len(rows)
	}
	c.metrics.observeUploadComplete(ctx, blob.UploadSize(), blob.DataSize(), totalShardRows*blob.RowSize(), len(sigs))

	span.SetStatus(codes.Ok, "")
	return SignedPaymentPromise{
		PaymentPromise:      promise,
		ValidatorSignatures: sigs,
	}, nil
}

// signerKey retrieves the secp256k1 public key from the keyring for the given key name.
func (c *Client) signerKey(keyName string) (*secp256k1.PubKey, error) {
	key, err := c.keyring.Key(keyName)
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

// signedPromise creates and signs a [PaymentPromise] using the given key name.
func (c *Client) signedPromise(ns share.Namespace, blob *Blob, height uint64, keyName string) (*PaymentPromise, error) {
	signerKey, err := c.signerKey(keyName)
	if err != nil {
		return nil, err
	}

	promise := &PaymentPromise{
		ChainID:           c.state.ChainID(),
		Height:            height,
		Namespace:         ns,
		UploadSize:        uint32(blob.UploadSize()),
		BlobVersion:       uint32(blob.Config().BlobVersion),
		Commitment:        blob.ID().Commitment(),
		CreationTimestamp: c.clock.Now().UTC(),
		SignerKey:         signerKey,
	}

	signBytes, err := promise.SignBytes()
	if err != nil {
		return nil, fmt.Errorf("getting sign bytes: %w", err)
	}

	// sign using the specified key and direct mode
	signature, _, err := c.keyring.Sign(keyName, signBytes, txsigning.SignMode_SIGN_MODE_DIRECT)
	if err != nil {
		return nil, fmt.Errorf("signing payment promise: %w", err)
	}

	promise.Signature = signature
	return promise, nil
}

// uploadTo uploads a shard to one validator and records its signature in
// sigSet. Returns whether sigSet has reached quorum.
func (c *Client) uploadTo(
	ctx context.Context,
	val *core.Validator,
	rowIndices []int,
	req *types.UploadShardRequest,
	blob *Blob,
	sigSet *validator.SignatureSet,
) bool {
	if ctx.Err() != nil {
		return false
	}

	ctx, cancel := context.WithCancel(ctx) // GRPC calls require context cancelling upon completion
	defer cancel()

	log := c.log.With(
		"validator", val.Address.String(),
		"blob_commitment", blob.ID().Commitment(),
		"rows_count", len(rowIndices),
	)

	uploadOk := false
	uploadStart := time.Now()
	valAddrStr := val.Address.String()

	ctx, span := c.tracer.Start(ctx, "upload_to",
		trace.WithAttributes(
			attribute.String("validator_address", valAddrStr),
			attribute.Int("rows_count", len(rowIndices)),
		),
	)
	defer span.End()
	defer func() {
		c.metrics.observeUploadTo(ctx, uploadStart, uploadOk, blob.UploadSize(), valAddrStr)
	}()

	// Generating row proofs is non-trivial, so build them lazily — only once
	// ClientCache.Request has acquired a working client. When the host can't be
	// resolved (e.g. an invalid registration no refresh can fix) the closure
	// never runs and we skip the work entirely. A non-nil req.Shard.Rows means
	// they were already built on a prior attempt, so a retry against a corrected
	// host reuses the same shard.
	buildRows := func() error {
		if req.Shard.Rows != nil {
			return nil
		}
		blobRows := make([]types.BlobRow, len(rowIndices))
		req.Shard.Rows = make([]*types.BlobRow, len(rowIndices))
		i := 0
		if err := blob.RowProofs(rowIndices, func(index int, row []byte, proof [][]byte) {
			br := &blobRows[i]
			br.Index = uint32(index)
			br.Data = row
			br.Proof = proof
			req.Shard.Rows[i] = br
			i++
		}); err != nil {
			return fmt.Errorf("generating row proofs: %w", err)
		}
		span.AddEvent("proofs_added")
		return nil
	}

	var resp *types.UploadShardResponse
	rpcStart := time.Now()
	err := c.clientCache.Request(ctx, val, func(client fibregrpc.Client) error {
		if err := buildRows(); err != nil {
			return err
		}
		rpcCtx, rpcCancel := context.WithTimeout(ctx, c.Config.RPCTimeout)
		defer rpcCancel()
		var err error
		resp, err = client.UploadShard(rpcCtx, req)
		return err
	})
	c.metrics.observeUploadToRPC(ctx, rpcStart, err == nil, valAddrStr)
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

	uploadOk = true
	log.DebugContext(ctx, "successfully uploaded to validator")
	span.AddEvent("signature_verified")
	span.SetStatus(codes.Ok, "")
	return hasEnough
}

// uploadShards fans out shard requests to all validators and returns when
// quorum is reached, all responses are in, or ctx is done. Background
// goroutines continue best-effort delivery to remaining peers past quorum;
// they are tracked via [c.closeWg] and unwind on client stop or caller cancel.
// The terminal goroutine releases the internal refcount via [Blob.release];
// pool storage is freed once both that release and Client.Upload's deferred
// [Blob.Free] of the user reference have fired.
func (c *Client) uploadShards(
	ctx context.Context,
	shardMap validator.ShardMap,
	requests map[*core.Validator]*types.UploadShardRequest,
	blob *Blob,
	sigSet *validator.SignatureSet,
) error {
	blob.retain()
	if len(requests) == 0 {
		blob.release()
		return nil
	}

	var (
		responses            atomic.Uint32
		responsesExhaustedCh = make(chan struct{})
		sigsCollectedOnce    atomic.Bool
		sigsCollectedCh      = make(chan struct{})
	)

	// spawn unconditionally even under ctx cancellation: each goroutine exits
	// fast via uploadTo(ctx) and runs its defer, so the "last one frees" path
	// fires naturally without a separate drain step.
	for val, req := range requests {
		c.closeWg.Add(1)
		go func(val *core.Validator, req *types.UploadShardRequest) {
			defer func() {
				if int(responses.Add(1)) == len(requests) {
					close(responsesExhaustedCh)
					blob.release()
				}
				c.closeWg.Done()
			}()

			hasEnough := c.uploadTo(ctx, val, shardMap[val], req, blob, sigSet)
			if hasEnough && sigsCollectedOnce.CompareAndSwap(false, true) {
				close(sigsCollectedCh)
			}
		}(val, req)
	}

	// No ctx.Done case: returning early on cancel would let Upload's deferred
	// [Blob.Free] race the in-flight uploadTo goroutines still referencing
	// pooled (potentially mmap'd) row buffers via the gRPC request, which
	// can segfault. Cancellation propagates through uploadTo's ctx.Err()
	// check, so all goroutines drain and responsesExhaustedCh fires.
	select {
	case <-responsesExhaustedCh: // every goroutine finished; terminal Free already fired
		if ctx.Err() != nil {
			return ctx.Err()
		}
	case <-sigsCollectedCh: // detach: remaining goroutines finish in background
	}
	return nil
}

// makeUploadRequests builds the per-validator request envelopes — the shared
// promise and RLC coefficients. The shard's rows (data + proofs) are built
// per validator by uploadTo, in the fan-out goroutines.
func makeUploadRequests(
	shardMap validator.ShardMap,
	pbPromise *types.PaymentPromise,
	rlcs rlc.Vector,
) map[*core.Validator]*types.UploadShardRequest {
	rlcsBytes := rlc.Marshal(rlcs)

	reqs := make([]types.UploadShardRequest, len(shardMap))
	shards := make([]types.BlobShard, len(shardMap))
	requests := make(map[*core.Validator]*types.UploadShardRequest, len(shardMap))
	i := 0
	for val := range shardMap {
		shards[i].Rlcs = rlcsBytes
		reqs[i].Promise = pbPromise
		reqs[i].Shard = &shards[i]
		requests[val] = &reqs[i]
		i++
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
