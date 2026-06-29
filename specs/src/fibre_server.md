# Fibre Server

This document describes the Fibre server implemented by the `fibre.Server` type. The server is a validator-operated gRPC service that accepts assigned blob shards, verifies payment promises and row proofs, stores shards locally until the payment promise expires, signs the payment promise with the validator consensus key, and serves stored shards back to download clients.

## Public gRPC API

The implemented data-plane service is `celestia.fibre.v1.Fibre`:

```protobuf
service Fibre {
  rpc UploadShard(UploadShardRequest) returns (UploadShardResponse);
  rpc DownloadShard(DownloadShardRequest) returns (DownloadShardResponse);
}

message BlobRow {
  uint32 index = 1;
  bytes data = 2;
  repeated bytes proof = 3;
}

message BlobShard {
  repeated BlobRow rows = 1;
  bytes rlcs = 2;
}

message UploadShardRequest {
  PaymentPromise promise = 1;
  BlobShard shard = 2;
}

message UploadShardResponse {
  bytes validator_signature = 1;
}

message DownloadShardRequest {
  bytes blob_id = 1;
}

message DownloadShardResponse {
  BlobShard shard = 1;
}
```

There is no Fibre server `FibreAccount` API and no server-side `PaymentProcessor` relay API in the current implementation. Escrow account operations, `MsgPayForFibre`, and timeout settlement are handled through the normal app query and transaction clients.

## PaymentPromise

The server uses the `celestia.fibre.v1.PaymentPromise` protobuf from `x/fibre`:

```protobuf
message PaymentPromise {
  string chain_id = 1;
  int64 height = 2;
  bytes namespace = 3;
  uint32 blob_size = 4;
  uint32 blob_version = 5;
  bytes commitment = 6;
  google.protobuf.Timestamp creation_timestamp = 7;
  cosmos.crypto.secp256k1.PubKey signer_public_key = 8;
  bytes signature = 9;
}
```

The internal `fibre.PaymentPromise` treats `signer_public_key` as the escrow-owner secp256k1 public key. `blob_size` is the padded upload size used by Fibre, not the raw user payload size. The supported blob version is currently `0`.

## Sign Bytes

Both the escrow owner and validators sign the same CometBFT raw-bytes domain:

```text
SignBytes = RawBytesMessageSignBytes(chain_id, "fibre/pp:v0", stripped)

stripped =
  signer_public_key_compressed_33 ||
  namespace_29 ||
  upload_size_u32be ||
  commitment_32 ||
  blob_version_u32be ||
  height_u64be ||
  creation_timestamp_utc_go_binary
```

`PaymentPromise.Hash()` is `SHA256(SignBytes || escrow_owner_signature)`. Validator signatures are produced with the validator consensus signer via `PrivValidator.SignRawBytes(chain_id, "fibre/pp:v0", stripped)` and must be ed25519 signature length.

## Construction And Configuration

`NewServer` validates `ServerConfig`, constructs the app state client, creates metrics, allocates the verifier pool, and binds the gRPC listener.

```go
type ServerConfig struct {
    AppGRPCAddress      string
    ServerListenAddress string
    SignerGRPCAddress   string
    UploadVerifyWorkers int

    StoreConfig

    LivenessThreshold   cmtmath.Fraction
    MinRowsPerValidator int
    MaxMessageSize      int

    StoreFn      func(StoreConfig) (*Store, error)
    StateClientFn func() (state.Client, error)
    SignerFn     func(chainID string) (core.PrivValidator, error)

    Log    *slog.Logger
    Tracer trace.Tracer
    Meter  metric.Meter
}
```

Defaults:

```text
app_grpc_address = "127.0.0.1:9090"
server_listen_address = "0.0.0.0:7980"
signer_grpc_address = "127.0.0.1:26659"
upload_verify_workers = runtime.GOMAXPROCS(0)
```

`StoreConfig.Path` is not a TOML field; the standalone `fibre start` command sets it from `--home`. The default state client is a gRPC app client connected to `AppGRPCAddress`. The default signer is a PrivValidatorAPI gRPC client connected to `SignerGRPCAddress`. Both app-node gRPC and signer gRPC use insecure local transport and are expected to be loopback or otherwise protected.

## Lifecycle

`Server.Start` starts the state client first, detects the chain ID, creates the signer, builds a TLS certificate endorsed by the validator consensus key, registers the Fibre gRPC service with TLS 1.3 credentials and max send/receive message sizes, opens the store, starts the prune loop, and starts serving gRPC in the background.

`Server.Stop` cancels the prune loop, stops the gRPC server, closes the signer if it implements `io.Closer`, closes the store, and stops the state client.

## Transport Security

The Fibre server-to-client gRPC link is TLS-only. On startup the server generates an ephemeral TLS keypair and uses the validator consensus signer to endorse that TLS public key. Clients verify the presented TLS key against the expected validator consensus public key and chain ID. There is no client certificate requirement and no mTLS. `DownloadShard` is public to any reachable client that can complete the server-authenticated TLS handshake.

## State Client

The server depends on `state.Client` for chain ID, validator sets, validator host lookup, and payment-promise state validation. The default implementation is `fibre/internal/grpc.AppClient`, which uses app-node gRPC. Validator sets are fetched through the CometBFT Block API `ValidatorSet` endpoint. Payment promises are checked with the app `x/fibre` `ValidatePaymentPromise` query, which returns the expiration time used for local pruning.

## UploadShard Flow

`UploadShard` performs the following work:

1. Convert the protobuf payment promise into the internal `fibre.PaymentPromise`.
2. Check that `promise.chain_id` matches the connected app chain ID.
3. Check that `promise.blob_version` is supported.
4. Run stateless promise validation: signer public key exists and is 33 bytes, chain ID is non-empty and at most 20 bytes, upload size is positive, creation timestamp is nonzero, escrow-owner signature is 64 bytes, height is positive, and the escrow-owner secp256k1 signature verifies against `SignBytes`.
5. Run stateful validation through the app state client. On success this returns `ExpiresAt`; the server computes `pruneAt = max(ExpiresAt, creation_timestamp + ShardRetention)`, so shards are kept for at least the locally configured retention (default 4h) and are never pruned while the promise is still valid.
6. Compute the payment-promise hash.
7. Fetch the validator set at `promise.height`.
8. Fetch this server's validator consensus public key from the signer and find it in the validator set.
9. Compute `validator.Set.Assign(promise.commitment, totalRows, originalRows, minRows, livenessThreshold)`.
10. Verify the uploaded row indices exactly match this validator's assignment by count, membership, and duplicate checks.
11. Validate the shard: all rows must be present and share one nonzero row size, each row must include data and proof, `promise.blob_size` must equal `row_size * originalRows`, `shard.rlcs` must unmarshal, and `rsema1d.Verifier.Verify` must accept the commitment, row proofs, and RLC vector.
12. Store the promise and shard.
13. Sign the payment promise with the validator signer and return the validator signature.

The server stores before signing. A successful validator signature means the server accepted and stored the shard.

## Assignment

Assignment is not a base/remainder split over a non-overlapping permutation. The implementation computes rows per validator from voting power and the liveness threshold:

```text
rows = ceil(originalRows * votingPower * livenessThreshold.denominator / (totalVotingPower * livenessThreshold.numerator))
rows = min(max(rows, minRows), originalRows)
```

The row indices `0..totalRows-1` are shuffled with a ChaCha8 RNG seeded by the commitment. Validators are then walked in CometBFT validator-set order and assigned the next `rows` shuffled indices. If the total assigned rows exceed `totalRows`, assignment wraps modulo `totalRows`, so the same row may be assigned to multiple validators.

## DownloadShard Flow

`DownloadShard` accepts a 33-byte `BlobID` (`blob_version || commitment`), validates the blob ID and supported blob version, looks up a stored shard by commitment, and returns the first matching stored `BlobShard`. If there are multiple promises for the same commitment, the store returns one deterministic matching shard rather than concatenating all rows for all promises. Missing data returns gRPC `NotFound`.

## Storage

The store uses Pebble for metadata and flat files for bulk shard payloads. The layout under `StoreConfig.Path` is:

```text
shards/<commitment-hex>-<promise-hash-hex>  finalized shard payload
staging/<random>                            temporary in-flight write
```

Pebble metadata keys are:

```text
/pp/<promise-hash-hex>                         protobuf PaymentPromise
/shard/<commitment-hex>/<promise-hash-hex>     shard marker
/prune/<YYYYMMDDHHmm>/<commitment>/<hash>      prune index
```

`Store.Put` writes the shard to a random staging file, writes metadata, then renames the staging file to the canonical shard file. Puts for the same commitment but different payment promises are stored independently by promise hash. `Store.Get(commitment)` iterates `/shard/<commitment>/` and returns the first readable shard file. If it finds an orphan marker whose shard file is missing, it deletes the marker lazily. `Store.PruneBefore` iterates the ordered `/prune/` index and deletes shard files, shard markers, payment promises, and prune entries whose prune timestamp is older than the cutoff.

## Pruning

The only background worker in the server is the prune loop. It runs once per minute and calls `Store.PruneBefore(time.Now())`, deleting shards whose `pruneAt` has passed. `pruneAt` is `max(ExpiresAt, creation_timestamp + ShardRetention)`, where `ShardRetention` is a local server setting (`shard_retention`, default 4h) independent of the chain's `PaymentPromiseTimeout`. There is no block subscriber, no local unprocessed-to-processed promotion, and no timeout scanner that submits `MsgPaymentPromiseTimeout`.

## Error Mapping

Current gRPC status behavior is intentionally simple:

| RPC | Condition | Status |
| --- | --- | --- |
| `UploadShard` | payment promise conversion, chain ID, blob version, stateless validation, or stateful validation fails | `InvalidArgument` |
| `UploadShard` | assignment verification fails | `InvalidArgument` |
| `UploadShard` | row, proof, RLC, upload-size, or commitment verification fails | `InvalidArgument` |
| `UploadShard` | store write or validator signing fails | `Internal` |
| `DownloadShard` | invalid blob ID or unsupported blob version | `InvalidArgument` |
| `DownloadShard` | no shard found for commitment | `NotFound` |
| `DownloadShard` | store read failure | `Internal` |

The implementation does not currently return `FailedPrecondition`, `PermissionDenied`, `AlreadyExists`, or `ResourceExhausted` for the cases described by older target designs, and responses do not include machine-readable error details or backoff hints.

## Concurrency And DoS Controls

The server does not implement per-peer token buckets, throughput caps, request backoff hints, or explicit upload/download RPC concurrency limits. Upload verification concurrency is bounded by `UploadVerifyWorkers`, which is the size of the pooled `rsema1d.Verifier` channel. gRPC receive/send message size is bounded by `MaxMessageSize` from protocol params.

## Metrics

The server records OpenTelemetry metrics for:

- `fibre.server.upload_shard.in_flight`
- `fibre.server.upload_shard.duration`
- `fibre.server.upload_shard.bytes`
- `fibre.server.download_shard.in_flight`
- `fibre.server.download_shard.duration`
- `fibre.server.download_shard.bytes`
- `fibre.server.store.put.duration`
- `fibre.server.store.get.duration`
- `fibre.server.sign.duration`
- `fibre.server.prune.entries`
- `fibre.server.prune.duration`
