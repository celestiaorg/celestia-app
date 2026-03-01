# Fibre Client

## 0) Glossary

* **FSP**: validator‑operated Fibre Service Provider.
* **DFSP**: preferred endpoint for non‑quorum ops (escrow queries; PFF relay).
* **Commitment**: `SHA256(rowRoot || rlcRoot)`.
* **PaymentPromise (PP)**: per `x/fibre`, fields `{signer, namespace, blob_size, commitment, fibre_blob_version=1, creation_timestamp:Timestamp, valset_height, signature}`.
* **Assignment**: permutation mapping `(commitment, valset@height)` → rows per validator.

## 1) Construction & Config

The client library can be constructed in two modes:

1. Light-node backed. This mode will run a light-node in-process to fetch headers and validator sets as needed.
2. Connected to a full node or a light node via RPC. This mode will query the node for headers and validator sets as needed.

```go
// Light‑node sub-module (embedded into light client as module. Uses light nodes header module to track valsets)
NewFibdreDAModule(cfg ModuleConfig, vtMode ValTrackerMode, opts ...Option) (*Client, error)

// RPC‑backed (fetch headers/valsets via RPC)
NewFibdreDAClient(cfg ClientConfig, vtMode ValTrackerMode, opts ...Option) (*Client, error)
```

### Key config

* `ChainID string` (MUST be in validator sign‑bytes domain)
* `EscrowOwner string` (bech32 signer)
* `DefaultFSPs []string` (grpc endpoints)

### Options

* `WithSendWorkers(int)`, `WithReadWorkers(int)` — concurrency controls

### Sub‑components

* **ValTracker** — track current validator set (height, members, power).
* **Keystore** — PP signer key (sdk.Keyring).
* **Encoder** — rsema1d encode/decode, commitment, proofs.
* **ShardMap** — Assignment implementation.
* **FSP Conns** — pooled gRPC clients (Fibre, FibreAccount, PaymentProcessor).
  * **FibreClient**: UploadRows, GetRows.
* **DFSP Conn** — preferred consensus endpoint(s) for `MsgPayForFibre` and escrow queries.
  * **FibrePaymentsClient**: SubmitPayForFibre
  * **FibreAccountClient**: escrow account balance, deposit, withdraw

## 2) Public API

```go
// Mirror from go-square
type Namespace struct {
  Version uint8  // MUST be 2
  Bytes   [29]byte
}

type PFFConfirmation struct {
  TxHash string
  Height uint64
}

type PutResult struct {
  Commitment          [32]byte
  ValidatorSignatures [][]byte // ed25519 over preimage (see §3)
  TTL                 *time.Time
  PFFConfirmation     PFFConfirmation
}

type Client interface {
  // Builds PP internally, uploads, aggregates sigs, submits MsgPayForFibre.
	// Errors:
	//`ErrInvalidNamespace`: namespace is not version 2 or not 29 bytes.
    //`ErrOversizeBlob`: data exceeds 128 MiB.
    //`ErrInsufficientBalance` from FSP: not enough balance; client should not retry without increasing balance of escrow account. Response should include current balance and state proofs.
    //`ErrNotEnoughSignatures`: not enough FSPs responded with valid signatures. Error will specify if power or count threshold was not met. Should include the number of valid signatures received.
    //`ErrPFFSubmission`: submission of MsgPayForFibre failed (all FSPs).
    //`Ctx` errors: timeouts, cancellations.
  Put(ctx context.Context, ns Namespace, data []byte) (PutResult, error)

  // Retrieves and reconstructs data by commitment.
    // Errors:
    // `ErrCommitmentNotFound`: no FSP had any rows for the commitment.
     // `ErrNotEnoughRows`: not enough rows were retrieved to reconstruct the original data.
     // `ErrRLCMismatch`: RLC computed from retrieved rows does not match the commitment.
     // `Ctx` errors: timeouts, cancellations.
  Get(ctx context.Context, ns Namespace, commitment [32]byte) ([]byte, error)

  // Access to escrow account management API.
  Account() AccountClient

  // Closes all connections and resources.
  Close() error
}

// AccountClient provides access to the DFSP's FibreAccount gRPC service.
type AccountClient interface {
// Mirrors: FibreAccount.QueryEscrowAccount
// Queries the escrow account for `signer`. Returns current & available balance.
QueryEscrowAccount(ctx context.Context, signer string) (curr_balance,avail_balance uint64,  err error)

// Mirrors: FibreAccount.Deposit
// Deposits `amount` into the escrow account for `signer`. Returns new balance after deposit.
Deposit(ctx context.Context, signer string, amount sdk.Coin) (balance uint64, error)

// Mirrors: FibreAccount.Withdraw
// Requests a withdrawal of `amount` from the escrow account for `signer`.
Withdraw(ctx context.Context, signer string, amount sdk.Coin) (*PendingWithdrawal, error)

// Mirrors: FibreAccount.PendingWithdrawals
// Lists pending withdrawals for `signer`.
PendingWithdrawals(ctx context.Context, signer string) ([]PendingWithdrawal, error)
}
```

## 3) Sign‑bytes for validator signatures

Validator signatures are over the PP preimage + ChainID domain tag. APIs use `google.protobuf.Timestamp` for `creation_timestamp`.

```text
SignBytes = SHA256(
  "fibre/pp:v1" || Chain_id || signer_bytes || namespace ||
  blob_size_u32be || commitment || fibre_blob_version_u32be ||
  creation_timestamp_pb || valset_height_u64be
)
```

* `signer_bytes`: 20‑byte account address (raw).
* `namespace`: 29 bytes (version MUST be 2).
* **Signature scheme**: ed25519.

## 4) Assignment (non‑overlapping; permutation‑based)

ShardMap: Assignment(commitment, valset@height) → map[validator]rows
Inputs: `seed = commitment`, `n = 16384`, validators `k = |valset@PP.creation_height|`.

1. **Permute shares**: sort `i` by `SHA256("share" || seed || u32be(i))`.
2. **Index validators** lexicographically by `PubKey` → indices `j=0..k-1`.
3. **(OPTIONAL) Permute validators**: sort `j` by `SHA256("validator" || seed || u32be(j))`.
4. `base = n // k`, `r = n % k`; first `r` in chosen validator order get `base+1`, others `base`.
5. Walk permuted shares handing contiguous blocks to validators in that order.

## 6) ValTracker

### API

```go
type ValTracker interface {
  // CurrentSet returns the validator set and height at the latest known block.
  CurrentSet(ctx context.Context) (vals []Validator, height int64, err error)

  // Stop stops any background processes (if applicable).
  Stop() error
}

type Validator struct {
    Address     Address      // Address is hex bytes. From Tendermint heeader
    PubKey      crypto.PubKey
    VotingPower int64
    FSPAddr     net.IP // FSP IP address
}
```

### Modes

* `ValTrackerModeLight`: use embedded light client to track headers/valsets.
* `ValTrackerModeRPC`: use json-RPC to fetch headers/valsets from remote Light or Bridge node.

## 5) Flows

### Put()

1. Validate: `0 < len(data) ≤ 128 MiB`. Enforce `row_size ∈ {64B * 2^n | ≤ 32 KiB}`.
2. `vals, valset_height := vt.CurrentSet(ctx)`.
3. `creation_timestamp := now.UTC()` (Timestamp).
4. Encode: choose `row_size`, build `rows[]`, `proofs[]`, `rlc_orig`, compute `commitment`.
5. Construct PP: `{signer, ns(v2), blob_size, commitment, fibre_blob_version=1, creation_timestamp, valset_height}` + signer signature.
6. Assignment for `valset_height`.
7. Fan‑out uploads to assigned FSPs (≤ `send_workers`): `UploadRowsRequest{promise, commitment, rows subset + proofs, rlc_orig}` → collect `validator_signature`s.
8. Verify received signatures.
9. Aggregate until ≥2/3 by **power** **and** by **count**.
10. Submit `MsgPayForFibre` via DFSP; fail over to other FSPs on relay failure (best‑effort).
11. Return `PutResult`.

**Note:** No proactive timeout path; normal timeout processing is handled by servers/caller.

### Get()

1. Get Valset: `vals, _ := vt.CurrentSet(ctx)`.
   * This could be a different validator set to the the validator set that actually has the shares. It probably won't be problematic because the validator sets will likely have a high degree of overlap and the erasure coding ensures enough redundancy
2. Send `GetRowsRequest{commitment}` to FSPs in parallel (≤ `read_workers`).
3. Collect `GetRowsResponse{rows[], rlc_orig_coefs}` from each FSP in parallel; Where rlc_orig_coefs should match only returned rows and have inclusion proofs. Verify all merkle proofs against `commitment`.
4. Decode data once amount of collected rows > `original_rows`; cancel remaining ongoing requests. recompute & verify **RLC**.
5. Return `data` or error.

## 6) Account Management API (client ↔ DFSP)

The client exposes `Account()` which returns an `AccountClient` bound to the **Default FSP (DFSP)** and mapped 1:1 to the server's `FibreAccount` gRPC service. Protobuf request/response messages and on‑chain semantics are defined by the payments spec (`x/fibre`); the client must not diverge.

### 6.1 Transport & Routing

* **Endpoint**: DFSP gRPC connection from client config. **Best‑effort** relay policy (no obligation for any given FSP to accept beyond availability).
* **Fallback**: if DFSP is unavailable, client MAY connect to any other FSP from config (same API).
* **Retries**: transient gRPC errors use exponential backoff with jitter.
* **Idempotency**:

  * `QueryEscrowAccount` & `PendingWithdrawals` are read‑only.
  * `Deposit`/`Withdraw` are **not** idempotent by default; callers must ensure they don't replay the same request.

### 6.2 Grpc Requests & Responses

Use the Protobuf messages from the payments spec:

* `QueryEscrowAccountRequest` → `QueryEscrowAccountResponse`
* `DepositRequest` → `DepositResponse`
* `WithdrawRequest` → `WithdrawResponse`
* `PendingWithdrawalsRequest` → `PendingWithdrawalsResponse`

TODO: Consider to include optional proofs for escrow state queries so clients can verify DFSP responses (or specify an alternative proof format). If proofs are provided, define verification rules here.

### 6.3 Errors

TODO: Map gRPC status codes to client errors

## 7) Client Defaults & Metrics

* `send_workers = 20`, `read_workers = 20`.
* Metrics: encode latency, chosen `row_size`, per‑FSP upload latency, signatures collected, quorum time, PFF submit/inclusion, balance cache age, insufficient‑proofs processed.
