# Fibre Client

This document describes the Fibre client as implemented in the `fibre` package. It is a v0 shard upload/download client backed by a celestia-app state client and validator-operated Fibre servers.

## 0) Glossary

* **Fibre server / FSP**: validator-operated gRPC service that stores and serves blob shards.
* **State client**: client dependency used to fetch chain ID, validator sets, validator Fibre hosts, and payment-promise validation state.
* **Blob**: encoded data plus a small v0 header, Reed-Solomon parity rows, row proofs, RLC vector, and commitment.
* **Commitment**: 32-byte rsema1d commitment over the row root and RLC root.
* **BlobID**: 33 bytes: `blob_version || commitment`.
* **PaymentPromise (PP)**: promise signed by the escrow owner and endorsed by validators after successful shard upload.
* **ShardMap**: deterministic mapping from `(commitment, validator set)` to row indices per validator.

## 1) Construction & Config

The client is constructed with a Cosmos SDK keyring and `ClientConfig`.

```go
func NewClient(kr keyring.Keyring, cfg ClientConfig) (*Client, error)
```

The configured key must exist in the keyring. `Start(ctx)` must be called before `Upload`, `Download`, or package-level `Put`.

```go
client, err := fibre.NewClient(kr, fibre.DefaultClientConfig())
if err != nil {
    return err
}
if err := client.Start(ctx); err != nil {
    return err
}
defer client.Stop(ctx)
```

### ClientConfig

```go
type ClientConfig struct {
    DefaultKeyName string
    StateAddress   string

    SafetyThreshold     cmtmath.Fraction
    LivenessThreshold   cmtmath.Fraction
    MinRowsPerValidator int
    MaxMessageSize      int
    RPCTimeout          time.Duration

    StateClientFn func() (state.Client, error)
    NewClientFn   fibregrpc.NewClientFn

    Log    *slog.Logger
    Tracer trace.Tracer
    Meter  metric.Meter
    Clock  clock.Clock
}
```

Defaults come from `DefaultProtocolParams`:

* `DefaultKeyName = "default-fibre"`
* `StateAddress = "127.0.0.1:9090"`
* `SafetyThreshold = 2/3`
* `LivenessThreshold = 1/3`
* `RPCTimeout = 15s`
* `MaxBlobSize = 128 MiB`
* original rows `K = 4096`
* total rows `K + N = 16384`
* supported blob version: `0`

If `StateClientFn` is nil, the client builds a gRPC app state client from `StateAddress`. If `NewClientFn` is nil, the client builds Fibre gRPC clients from validator hosts returned by the state client's host registry.

There are no `send_workers` or `read_workers` options in the current implementation. Upload starts one goroutine per assigned validator. Download dynamically starts validator requests until enough rows are in flight or retrieved.

## 2) Public API

### Client Lifecycle

```go
func (c *Client) Start(ctx context.Context) error
func (c *Client) Stop(ctx context.Context) error
func (c *Client) Await()
func (c *Client) ChainID() string
```

`Stop` marks the client closed, waits for in-flight upload/download work unless its context is canceled, and closes cached Fibre gRPC connections. `Await` waits for upload/download goroutines without closing the client.

### Blob API

```go
func NewBlob(data []byte, cfg BlobConfig) (*Blob, error)
func DefaultBlobConfigV0() BlobConfig

func (b *Blob) ID() BlobID
func (b *Blob) Data() []byte
func (b *Blob) DataSize() int
func (b *Blob) UploadSize() int
func (b *Blob) RowSize() int
func (b *Blob) Free()
```

`NewBlob` requires non-empty data. It returns `ErrBlobTooLarge` if `len(data)` exceeds `BlobConfig.MaxDataSize` (`128 MiB - 5` for the default v0 header). The returned blob owns pooled storage and must be released with `Free`.

### Upload

```go
type UploadOption func(*uploadOptions)

func WithKeyName(keyName string) UploadOption
func WithAwaitAllSignatures() UploadOption

func (c *Client) Upload(
    ctx context.Context,
    ns share.Namespace,
    blob *Blob,
    opts ...UploadOption,
) (SignedPaymentPromise, error)
```

`Upload` signs a payment promise with the configured key, uploads assigned row shards to validators, verifies validator signatures, and returns a `SignedPaymentPromise`.

By default, `Upload` returns after the safety threshold by voting power is reached. Remaining validator uploads continue in background and are tracked by `Await`/`Stop`. `WithAwaitAllSignatures` changes the threshold to all validator voting power and waits for all successful signatures.

```go
type SignedPaymentPromise struct {
    *PaymentPromise
    ValidatorSignatures [][]byte
}
```

### Put

```go
type PutResult struct {
    BlobID              BlobID
    ValidatorSignatures [][]byte
    TTL                 time.Time
    TxHash              string
    Height              uint64
}

func Put(
    ctx context.Context,
    c *Client,
    txClient *user.TxClient,
    ns share.Namespace,
    data []byte,
) (PutResult, error)
```

`Put` is a package-level convenience helper. It creates a v0 blob, calls `Client.Upload` to upload assigned shards to validators and collect validator signatures, builds `MsgPayForFibre`, broadcasts it through the supplied `user.TxClient`, and waits for transaction confirmation.

`Put` does not use a DFSP relay client. The caller supplies the transaction client, so transaction endpoint selection, account configuration, fees, fee grants, and signing setup are determined by that `user.TxClient`. `Put` submits one `MsgPayForFibre` for the blob; callers that need batching or custom transaction flow should call `Client.Upload` directly and submit payments themselves. `TTL` is currently present in `PutResult` but is not populated.

### Download

```go
type DownloadOption func(*downloadOptions)

func WithHeight(height uint64) DownloadOption

func (c *Client) Download(
    ctx context.Context,
    id BlobID,
    opts ...DownloadOption,
) (*Blob, error)
```

`Download` fetches and reconstructs a blob by `BlobID`. If `WithHeight` is provided, the client uses the validator set at that height; otherwise it uses the current head validator set. The returned blob owns pooled storage and must be released with `Free`.

The current API does not expose `Get(ctx, namespace, commitment) ([]byte, error)`. Callers use `Download(ctx, NewBlobID(version, commitment))` and then read `blob.Data()`.

## 3) Payment Promise and Sign Bytes

The implemented `PaymentPromise` is v0-oriented and uses a secp256k1 public key to identify the escrow owner.

```go
type PaymentPromise struct {
    SignerKey         *secp256k1.PubKey
    ChainID           string
    Namespace         share.Namespace
    UploadSize        uint32
    BlobVersion       uint32
    Commitment        Commitment
    CreationTimestamp time.Time
    Signature         []byte
    Height            uint64
}
```

The protobuf form is:

```proto
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

The signed payload is:

```text
stripped =
  signer_public_key_compressed_33 ||
  namespace_29 ||
  upload_size_u32be ||
  commitment_32 ||
  blob_version_u32be ||
  height_u64be ||
  creation_timestamp_go_binary_utc

SignBytes = CometBFT RawBytesMessageSignBytes(
  chain_id,
  "fibre/pp:v0",
  stripped,
)
```

The escrow-owner signature is a 64-byte secp256k1 signature over `SignBytes`. Validator signatures are ed25519 signatures over the same `SignBytes`.

`PaymentPromise.Hash()` is:

```text
SHA256(SignBytes || escrow_owner_signature)
```

## 4) Blob Encoding

Only blob version `0` is supported.

The encoded data starts with a five-byte header:

```text
version_u8 || original_data_size_u32be
```

Rows are produced with `rsema1d`. Default protocol parameters:

* original rows: `4096`
* parity rows: `12288`
* total rows: `16384`
* encoding ratio: `0.25`
* maximum blob size, including header: `128 MiB`
* minimum row-size alignment: `64` bytes

`UploadSize` is the padded original-row size only:

```text
UploadSize = row_size * original_rows
```

It excludes parity rows but includes padding and the v0 header.

## 5) Assignment

`validator.Set.Assign` maps row indices to validators using voting power and the configured liveness threshold.

Inputs:

* `commitment`
* `totalRows`
* `originalRows`
* `minRows`
* `livenessThreshold`
* validator set at the payment promise height

For each validator:

```text
rows = ceil(originalRows * validator_power * liveness_denominator /
            (total_voting_power * liveness_numerator))
rows = min(max(rows, minRows), originalRows)
```

Then:

1. Seed a ChaCha8 RNG with the commitment.
2. Shuffle all row indices `0..totalRows-1` with Fisher-Yates.
3. Walk validators in CometBFT validator-set order.
4. Give each validator the next `rows` shuffled row indices.
5. If the sum of assigned rows exceeds `totalRows`, wrap around modulo `totalRows`, which can assign the same row to multiple validators.

This is not an equal, non-overlapping permutation assignment. Overlap can occur when minimum-row guarantees over-assign rows.

## 6) State Client

The client depends on `state.Client`:

```go
type Client interface {
    validator.SetGetter
    validator.HostRegistry

    ChainID() string
    VerifyPromise(context.Context, *types.PaymentPromise) (VerifiedPromise, error)

    Start(context.Context) error
    Stop(context.Context) error
}
```

The default implementation is the gRPC `AppClient`. On `Start`, it detects the chain ID from the app node and pulls validator Fibre provider hosts from `x/valaddr`.

Validator sets are fetched through CometBFT's gRPC Block API:

```go
Head(ctx) (validator.Set, error)
GetByHeight(ctx, height uint64) (validator.Set, error)
```

There is no embedded light-node-backed client constructor in the current implementation.

## 7) gRPC Transport

The implemented Fibre service is shard-oriented:

```proto
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

service Fibre {
  rpc UploadShard(UploadShardRequest) returns (UploadShardResponse);
  rpc DownloadShard(DownloadShardRequest) returns (DownloadShardResponse);
}
```

`BlobShard.rlcs` contains the serialized full original-row RLC vector, not only coefficients for the returned rows.

The default Fibre gRPC client resolves a validator host from the state client's host registry and uses TLS with validator consensus-key identity verification.

## 8) Upload Flow

1. Require `Start` to have completed and the client not to be closed.
2. Retain the blob's pooled storage for the upload lifetime.
3. Fetch the current validator set with `state.Head(ctx)`.
4. Build and sign a `PaymentPromise` using the selected keyring key.
5. Compute `PaymentPromise.Hash()` for logging/storage identity.
6. Compute the `ShardMap` from the blob commitment and validator set.
7. Build one `UploadShardRequest` envelope per validator with the shared promise and serialized RLC vector.
8. Start one goroutine per validator.
9. For each validator, build row data and proofs for its assigned row indices, call `UploadShard` with `RPCTimeout`, parse the validator signature, and add it to the `SignatureSet`.
10. Return when all validators have responded or the configured voting-power threshold is reached.
11. Return `SignedPaymentPromise`.

Signature collection currently enforces voting-power threshold only. It does not enforce a separate count threshold.

## 9) Put Flow

`Put` is a convenience wrapper around upload plus app transaction submission:

1. Create a v0 `Blob` from `data`.
2. Call `Client.Upload` using `WithKeyName(txClient.DefaultAccountName())` to upload assigned shards to validators and collect validator signatures.
3. Convert the signed promise to proto.
4. Build `x/fibre` `MsgPayForFibre` with validator signatures.
5. Broadcast through `txClient.BroadcastTx`.
6. Wait for inclusion with `txClient.ConfirmTx`.
7. Return `PutResult`.

The implementation does not submit PFF through a Fibre payment relay service and does not implement DFSP fallback.

## 10) Download Flow

1. Require `Start` to have completed and the client not to be closed.
2. Validate `BlobID`.
3. Fetch a validator set:
   * `GetByHeight(ctx, height)` when `WithHeight(height)` is used.
   * `Head(ctx)` otherwise.
4. Select validators with `validator.Set.Select`, shuffled by stake for load balancing.
5. Start download workers while the reconstructor still wants rows and row reservations are available.
6. Each worker calls `DownloadShard` with `RPCTimeout`.
7. Parse rows, row proofs, and RLC vector from `BlobShard`.
8. Add the shard to the `rsema1d.Reconstructor`, which verifies proofs and the commitment/RLC relationship.
9. Stop dispatching after enough unique rows are collected or all selected validators have been tried.
10. Reconstruct and decode the v0 blob header.
11. Return a `Blob`.

Errors are classified as:

* `ErrNotFound`: no rows were retrieved.
* `ErrNotEnoughShards`: some rows were retrieved, but not enough to reconstruct.
* reconstruction/verification/decode errors for invalid shards or invalid data.
* context cancellation/deadline errors from the caller or per-RPC timeout.

## 11) Account and Escrow APIs

The Fibre client package does not currently expose `Account()` or an `AccountClient`.

Escrow state and transactions are available through the app's `x/fibre` query and msg services:

* `Query.EscrowAccount`
* `Query.Withdrawals`
* `Msg.DepositToEscrow`
* `Msg.RequestWithdrawal`
* `Msg.PayForFibre`
* `Msg.PaymentPromiseTimeout`

Callers that need deposits, withdrawals, escrow queries, or PFF transaction submission use the normal app gRPC query clients and transaction clients.

## 12) Errors

Important client-side errors include:

* `ErrClientClosed`
* `ErrKeyNotFound`
* `ErrBlobTooLarge`
* `ErrNotFound`
* `ErrNotEnoughShards`
* `validator.NotEnoughSignaturesError`

Other errors are returned as wrapped errors from keyring operations, state lookups, gRPC calls, row proof generation, reconstruction, decoding, or transaction broadcasting/confirmation.

There is no dedicated client error mapping for insufficient escrow balance, invalid namespace, PFF submission, or RLC mismatch.

## 13) Metrics

The current client records OpenTelemetry metrics for:

* upload in-flight count and duration
* uploaded padded bytes, original data bytes, and network bytes
* validator signatures collected
* per-validator upload duration and RPC latency
* download in-flight count, duration, and bytes
* per-validator download duration and RPC latency

It does not currently expose separate metrics for encode latency, chosen row size, quorum time, PFF submission/inclusion, balance cache age, or insufficient proof handling.
