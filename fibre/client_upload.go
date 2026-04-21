package fibre

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
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
// May keep uploading data in background after returning successfully.
// Returns [ErrClientClosed] if the client has been closed.
func (c *Client) Upload(ctx context.Context, ns share.Namespace, blob *Blob, opts ...UploadOption) (result SignedPaymentPromise, err error) {
	if !c.started.Load() {
		return result, errors.New("fibre client is not started")
	}
	if c.closed.Load() {
		return result, ErrClientClosed
	}

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
	requests := makeUploadRequests(shardMap, promiseProto, blob.RLCCoeffs())
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

// uploadTo uploads a blob shard to a single validator and adds the response
// signature to the signature set. It splits the network budget into two
// layers: a short DialTimeout for connection establishment (so a black-holed
// peer is shed fast) and a longer RPCTimeout for the UploadShard call.
//
// Returns:
//   - hasEnough: true if sigSet now holds enough signatures for quorum;
//   - err: non-nil on any failure (dial, proof, RPC, signature parse/add).
func (c *Client) uploadTo(
	ctx context.Context,
	val *core.Validator,
	req *types.UploadShardRequest,
	blob *Blob,
	sigSet *validator.SignatureSet,
) (hasEnough bool, err error) {
	log := c.log.With(
		"validator", val.Address.String(),
		"blob_commitment", blob.ID().Commitment(),
		"rows_count", len(req.Shard.Rows),
	)

	uploadOk := false
	uploadStart := time.Now()
	valAddrStr := val.Address.String()

	ctx, span := c.tracer.Start(ctx, "upload_to",
		trace.WithAttributes(
			attribute.String("validator_address", valAddrStr),
			attribute.Int("rows_count", len(req.Shard.Rows)),
		),
	)
	defer span.End()
	defer func() {
		c.metrics.observeUploadTo(ctx, uploadStart, uploadOk, blob.UploadSize(), valAddrStr)
	}()

	// Dial under a tight deadline so a network-black-holed validator
	// cannot park this goroutine on the TCP SYN retry window.
	dialCtx, dialCancel := context.WithTimeout(ctx, c.Config.DialTimeout)
	client, err := c.clientCache.GetClient(dialCtx, val)
	dialCancel()
	if err != nil {
		log.WarnContext(ctx, "can't get grpc.FibreClient", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "can't get grpc.FibreClient")
		return false, err
	}
	span.AddEvent("client_acquired")

	// get proofs and rows here in per request routine which is in parallel which ~39% faster for max blob size
	for i, rowPb := range req.Shard.Rows {
		row, rowErr := blob.Row(int(rowPb.Index))
		if rowErr != nil {
			log.WarnContext(ctx, "failed to generate proof for row", "row_index", rowPb.Index, "error", rowErr)
			span.RecordError(rowErr, trace.WithAttributes(attribute.Int("row_index", int(rowPb.Index))))
			span.SetStatus(codes.Error, "failed to generate proof for row")
			return false, rowErr
		}
		req.Shard.Rows[i].Data = row.Row
		req.Shard.Rows[i].Proof = row.RowProof.RowProof
	}
	span.AddEvent("proofs_generated")

	// Run the RPC under a separate (longer) deadline. Separating dial
	// from RPC means a healthy-but-slow peer is not killed by a tight
	// dial budget, while a silent peer is shed by the dial budget
	// before it gets near the RPC budget.
	rpcCtx, rpcCancel := context.WithTimeout(ctx, c.Config.RPCTimeout)
	defer rpcCancel()

	rpcStart := time.Now()
	resp, err := client.UploadShard(rpcCtx, req)
	c.metrics.observeUploadToRPC(ctx, rpcStart, err == nil, valAddrStr)
	if err != nil {
		log.WarnContext(ctx, "failed to upload rows", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to upload rows")
		return false, err
	}
	span.AddEvent("rows_uploaded")

	// validate and get signature
	signature, err := parseSignature(resp.ValidatorSignature)
	if err != nil {
		log.WarnContext(ctx, "failed to parse signature", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to parse signature")
		return false, err
	}

	// apply signature response and check if we have enough
	hasEnough, err = sigSet.Add(val, signature)
	if err != nil {
		log.WarnContext(ctx, "failed to add signature", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to add signature")
		return false, err
	}

	uploadOk = true
	log.DebugContext(ctx, "successfully uploaded to validator")
	span.AddEvent("signature_verified")
	span.SetStatus(codes.Ok, "")
	return hasEnough, nil
}

// uploadShards pushes assigned [types.BlobShard]s to all validators and
// collects signature responses. Returns when quorum is reached, all
// responses are in, or the caller's context is done.
//
// Design notes (2/3 quorum isolation):
//
//   - Fan-out is non-blocking: all goroutines are spawned up front.
//     The circuit breaker check happens INSIDE each goroutine, so a
//     slow or dead peer cannot delay other peers' goroutines from
//     starting. This is the root-cause fix for the single-black-holed-
//     validator throughput collapse that the old global RPC semaphore
//     caused by serializing the fan-out loop behind slot acquires.
//
//   - Circuit breaker: after a peer accumulates consecutive failures,
//     subsequent blob uploads skip that peer instantly for a cooldown
//     window — amortizing the dead-peer cost across many blobs.
//
//   - Best-effort post-quorum delivery: uploadShards returns as soon
//     as quorum is reached, but background fan-out goroutines continue
//     delivering shards to the remaining validators. More validators
//     holding the data means downloaders have more peers to read from.
//     Background goroutines are tracked via [c.closeWg] and inherit
//     the caller's ctx, so they unwind on client stop or caller cancel.
//
//   - Peer-failure attribution: the breaker records RPC failures unless
//     the caller's ctx is done — in which case the failure is likely
//     our teardown rather than a peer fault. This keeps flaky peers
//     penalized under normal load while not poisoning breaker state
//     during a graceful shutdown.
func (c *Client) uploadShards(
	ctx context.Context,
	requests map[*core.Validator]*types.UploadShardRequest,
	blob *Blob,
	sigSet *validator.SignatureSet,
) error {
	// Prune peer registry to the current validator set so rotated-out
	// peers don't hold breaker state, and newly-rotated-in peers with
	// reused addresses don't inherit a stale reputation.
	if len(requests) > 0 {
		keep := make(map[string]struct{}, len(requests))
		for val := range requests {
			keep[val.Address.String()] = struct{}{}
		}
		c.peers.prune(keep)
	}

	var (
		responses            atomic.Uint32
		responsesExhaustedCh = make(chan struct{})
		sigsCollectedOnce    atomic.Bool
		sigsCollectedCh      = make(chan struct{})
	)

	// Empty request map: nothing to do; the sigSet will surface the
	// "no signatures" error to the caller.
	if len(requests) == 0 {
		return nil
	}

	for val, req := range requests {
		c.closeWg.Add(1)
		go func(val *core.Validator, req *types.UploadShardRequest) {
			defer func() {
				if int(responses.Add(1)) == len(requests) {
					close(responsesExhaustedCh)
				}
				c.closeWg.Done()
			}()

			valAddrStr := val.Address.String()
			brk := c.peers.get(valAddrStr)

			// Circuit breaker: skip peers known to be dead. Their cost
			// is paid once (when the circuit first opens) and amortized
			// across subsequent blob uploads.
			allowed, halfOpenProbe := brk.Allow(c.clock.Now())
			if !allowed {
				return
			}
			// If Allow consumed the half-open probe slot we own the
			// obligation to either record an outcome or reset the
			// probe, or the breaker wedges in half-open forever.
			halfOpenHandled := false
			defer func() {
				if halfOpenProbe && !halfOpenHandled {
					brk.resetHalfOpen()
				}
			}()

			hasEnough, err := c.uploadTo(ctx, val, req, blob, sigSet)
			if err != nil {
				// Don't penalize a peer when the caller's ctx is done —
				// that's our teardown, not a peer fault. Any other
				// failure (including RPCTimeout hit) flows into the
				// breaker so flaky peers are still penalized.
				if ctx.Err() != nil {
					return
				}
				halfOpenHandled = true
				newState, changed := brk.RecordFailure(c.clock.Now())
				if changed {
					c.log.WarnContext(ctx, "circuit breaker state changed",
						"validator", valAddrStr,
						"new_state", newState.String(),
					)
				}
				return
			}
			halfOpenHandled = true
			newState, changed := brk.RecordSuccess()
			if changed {
				c.log.InfoContext(ctx, "circuit breaker state changed",
					"validator", valAddrStr,
					"new_state", newState.String(),
				)
			}

			if hasEnough && sigsCollectedOnce.CompareAndSwap(false, true) {
				close(sigsCollectedCh)
			}
		}(val, req)
	}

	select {
	case <-responsesExhaustedCh:
	case <-sigsCollectedCh:
		// Quorum reached; uploadShards returns, but background
		// goroutines continue best-effort delivery to remaining peers.
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
				Rows:         rows,
				Coefficients: rlcCoeffsBytes,
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
