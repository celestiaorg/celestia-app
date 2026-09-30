# Fibre Server

This document describes the Fibre server implemented by the `fibre.Server` type. The server is a validator-operated gRPC service that accepts assigned blob shards, verifies payment promises and row proofs, stores shards until the later of promise expiry and the retention deadline, signs the payment promise with the validator consensus key, and serves stored shards back to download clients.

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
    MinUploadSize       int
    UploadVerifyWorkers int
    MaxConnections      int
    MaxConcurrentStreams int

    StoreConfig

    LivenessThreshold   cmtmath.Fraction
    MinRowsPerValidator int
    OriginalRows        int
    MaxShardSize        int
    UnlimitedBudget     bool
    MaxMessageSize      int

    StoreFn      func(context.Context, StoreConfig) (*Store, error)
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
signer_grpc_address = "127.0.0.1:26669"
min_upload_size = 262144
upload_verify_workers = runtime.GOMAXPROCS(0)
max_connections = 16
max_concurrent_streams = 13
storage_backend = "local"
```

`StoreConfig.Path` is not a TOML field; the standalone `fibre start` command sets it from `--home`. The default state client is a gRPC app client connected to `AppGRPCAddress`. The default signer is a PrivValidatorAPI gRPC client connected to `SignerGRPCAddress`. Both app-node gRPC and signer gRPC use insecure local transport and are expected to be loopback or otherwise protected.

## Lifecycle

`Server.Start` starts the state client first, detects the chain ID, creates the signer, builds a TLS certificate endorsed by the validator consensus key, registers the Fibre gRPC service with TLS 1.3 credentials and max send/receive message sizes, opens the store, seeds occupancy from shard markers (falling back to file sizes for legacy local markers), derives its storage budget, starts the prune loop, and starts serving gRPC in the background.

`Server.Stop` cancels the prune loop, stops the gRPC server, closes the signer if it implements `io.Closer`, closes the store, and stops the state client.

## Transport Security

The Fibre server-to-client gRPC link is TLS-only. On startup the server generates an ephemeral TLS keypair and uses the validator consensus signer to endorse that TLS public key. Clients verify the presented TLS key against the expected validator consensus public key and chain ID. There is no client certificate requirement and no mTLS. `DownloadShard` is public to any reachable client that can complete the server-authenticated TLS handshake.

The endorsement travels in a custom, non-critical X.509 extension identified by OID `1.3.6.1.4.1.66463.1.1`, carrying a consensus-key signature over the TLS public key and validity window. The wire format, exact signed bytes, verifier rules, and golden test vectors are specified in [Fibre TLS Identity](./fibre_tls_identity.md).

### OID allocations

`1.3.6.1.4.1.66463` is the [IANA Private Enterprise Number 66463](https://www.iana.org/assignments/enterprise-numbers/?q=66463) arc used for Celestia protocol identifiers. Allocations:

| OID | Meaning |
|-----|---------|
| `1.3.6.1.4.1.66463.1` | Fibre |
| `1.3.6.1.4.1.66463.1.1` | Fibre TLS signed-identity certificate extension |

New allocations under this arc must be recorded in this table. The extension OID is a wire-level protocol constant: changing it breaks TLS handshakes between peers on different versions.

## State Client

The server depends on `state.Client` for chain ID, validator sets, validator host lookup, and payment-promise state validation. The default implementation is `fibre/internal/grpc.AppClient`, which uses app-node gRPC. Validator sets are fetched through the CometBFT Block API `ValidatorSet` endpoint. Payment promises are checked with the app `x/fibre` `ValidatePaymentPromise` query, which returns expiration and shard retention. By default the query also verifies the owner signature and reserves funds in a validator-local promise cache. This query-side reservation is separate from consensus settlement; see the [module reference](./fibre_module.md#queries).

## UploadShard Flow

`UploadShard` performs the following work:

1. Convert the protobuf payment promise into the internal `fibre.PaymentPromise`.
2. Check that `promise.chain_id` matches the connected app chain ID.
3. Check that `promise.blob_version` is supported.
4. Run stateless promise validation: signer public key exists and is 33 bytes, chain ID is non-empty and at most 20 bytes, upload size is positive, creation timestamp is nonzero, escrow-owner signature is 64 bytes, height is positive, and the escrow-owner secp256k1 signature verifies against `SignBytes`.
5. Reject a padded upload size below the local `MinUploadSize` (default 256 KiB), then run stateful validation through the app state client. On success this returns `ExpiresAt` and `ShardRetention` (the `x/fibre` on-chain, governance-changeable parameter, default 4h); the server computes `pruneAt = max(ExpiresAt, creation_timestamp + ShardRetention)`, so shards are kept for at least the configured retention and are never pruned while the promise is still valid.
6. Compute the payment-promise hash and require a non-nil shard. If the store already has this promise and shard, skip assignment verification, shard verification, and storage, and re-sign the promise. Otherwise continue below.
7. Fetch the validator set at `promise.height`.
8. Fetch this server's validator consensus public key from the signer and find it in the validator set.
9. Compute `validator.Set.Assign(promise.commitment, totalRows, originalRows, minRows, livenessThreshold)`.
10. Verify the uploaded row indices exactly match this validator's assignment by count, membership, and duplicate checks.
11. Validate the shard: all rows must be present and share one nonzero row size, each row must include data and proof, `promise.blob_size` must equal `row_size * originalRows`, `shard.rlcs` must unmarshal, and `rsema1d.Verifier.Verify` must accept the commitment, row proofs, and RLC vector.
12. Serialize uploads with the same promise hash, honoring cancellation while waiting, and re-check storage. Reserve encoded storage size for a shard not already accounted for by its marker, then store the promise and shard. Reject an over-budget upload with `ResourceExhausted` and `RetryInfo`; release a new reservation if storage fails.
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

The store uses Pebble for metadata. `StorageBackend` selects local flat files (the default) or experimental object storage for new shard payloads. Each shard marker records its backend and encoded size, so existing shards remain routed to their original backend after a configuration change. Object storage must remain configured and accessible until its shards are pruned. See [`StoreConfig`](../../fibre/store_config.go) and [`ObjectStorageConfig`](../../fibre/store_object_config.go).

For local payloads, the layout under `StoreConfig.Path` is:

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

`Store.Put` writes the payload to the selected backend, then commits the Pebble metadata batch. The local backend stages and renames the file before that commit; object storage does not use the local staging directory. Puts for the same commitment but different payment promises are stored independently by promise hash. `Store.Get(commitment)` iterates `/shard/<commitment>/` and returns the first readable shard from the backend selected by its marker. Markers whose payloads are missing remain until pruning so their recorded occupancy can be released. `Store.PruneBefore` iterates the ordered `/prune/` index and deletes expired payloads from their recorded backend and then removes the associated metadata. Failed payload deletions retain metadata for a later retry.

## Pruning

The only background worker in the server is the prune loop. It runs once per minute, prunes expired entries in batches, releases occupancy for successfully pruned entries, and recomputes the storage budget from current stake and governance parameters. A failed budget refresh keeps the previous budget. `pruneAt` is `max(ExpiresAt, creation_timestamp + ShardRetention)`, where `ShardRetention` is the `x/fibre` on-chain, governance-changeable parameter (default 4h) independent of the chain's `PaymentPromiseTimeout`. There is no block subscriber, no local unprocessed-to-processed promotion, and no timeout scanner that submits `MsgPaymentPromiseTimeout`.

## Error Mapping

Current gRPC status behavior is intentionally simple:

| RPC | Condition | Status |
| --- | --- | --- |
| `UploadShard` | payment promise conversion, chain ID, blob version, stateless validation, or stateful validation fails | `InvalidArgument` |
| `UploadShard` | assignment verification fails | `InvalidArgument` |
| `UploadShard` | row, proof, RLC, upload-size, or commitment verification fails | `InvalidArgument` |
| `UploadShard` | storage budget exceeded | `ResourceExhausted`, with `RetryInfo` |
| `UploadShard` | store write aborted by caller cancellation or deadline | `Canceled` or `DeadlineExceeded` |
| `UploadShard` | store write or validator signing otherwise fails | `Internal` |
| `DownloadShard` | invalid blob ID or unsupported blob version | `InvalidArgument` |
| `DownloadShard` | no shard found for commitment | `NotFound` |
| `DownloadShard` | store read failure | `Internal` |

Storage-budget rejection includes a retry delay of one prune interval plus jitter: at least 60 seconds and less than 90 seconds. Errors returned by app-side promise validation, including local promise-cache budget rejection, are wrapped as `InvalidArgument` by `UploadShard`; they do not use this storage retry response.

## Concurrency And DoS Controls

The server limits stored and in-flight shard bytes. Its budget is `FullStakeStorageBudget * assignedRows / OriginalRows`, using the current validator set and the same row-count calculation as assignment. `UnlimitedBudget` disables this limiter. This is a shard-payload occupancy budget, not a limit on total filesystem usage including Pebble metadata. The transport defaults to 16 connections and 13 concurrent HTTP/2 streams per connection, configurable through `MaxConnections` and `MaxConcurrentStreams`, with a 15-second connection setup timeout and keepalive limits. These limits apply before shard verification. There are no per-peer token buckets or throughput caps. Upload verification concurrency is bounded by `UploadVerifyWorkers`, which is the size of the pooled `rsema1d.Verifier` channel. gRPC receive/send message size is bounded by `MaxMessageSize` from protocol params.

## Metrics

The server records OpenTelemetry metrics for:

- `fibre.server.upload_shard.in_flight`
- `fibre.server.upload_shard.duration`
- `fibre.server.upload_shard.bytes`
- `fibre.server.upload_shard.dupe_hits`
- `fibre.server.upload_shard.last_success_timestamp`
- `fibre.server.upload_shard.rejected`
- `fibre.server.upload_shard.occupancy_bytes`
- `fibre.server.upload_shard.budget_bytes`
- `fibre.server.download_shard.in_flight`
- `fibre.server.download_shard.duration`
- `fibre.server.download_shard.bytes`
- `fibre.server.store.put.duration`
- `fibre.server.store.get.duration`
- `fibre.server.backend.get.duration`
- `fibre.server.backend.get.in_flight`
- `fibre.server.backend.get.bytes`
- `fibre.server.sign.duration`
- `fibre.server.prune.entries`
- `fibre.server.prune.duration`

Backend GET metrics record only the primary backend and use `backend=local|object`.
Duration is measured in seconds through payload reading, decoding and closing, with `outcome=success|not_found|timeout|canceled|throttled|error`.
The in-flight metric counts concurrent backend GET calls. The byte counter records encoded bytes consumed, including partial failures and buffered reads.
Each observation covers one backend call. Object GET duration includes SDK retries; the outcome describes the final result.
