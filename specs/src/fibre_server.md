# Fibre Server

## 1) Public gRPC APIs

### 1.1 Fibre API (data plane)

```proto
syntax = "proto3";
package fibre.v1;

import "google/protobuf/timestamp.proto";

message GF128 { bytes coeffs = 1; }                       // len == 16
message Commitment { bytes value = 1; }                   // len == 32
message RowWithProof { uint32 index = 1; bytes row = 2; bytes proof = 3; }
message RlcOrig { repeated GF128 coeffs = 1; }            // len == N

// Mirrors x/fibre.PaymentPromise (names unified, Timestamp used)
message PaymentPromise {
  string signer = 1;                                      // bech32 escrow owner
  bytes namespace = 2;                                    // 29 raw bytes
  uint32 blob_size = 3;                                   // original length (pre-padding)
  bytes commitment = 4;                                   // 32 bytes
  uint32 fibre_blob_version = 5;                          // = 1
  google.protobuf.Timestamp creation_timestamp = 6;       // UTC
  uint64 valset_height = 7;                               // assignment height
  bytes signature = 8;                                    // signer signature over PP sign-bytes
}

message UploadRowsRequest {
  PaymentPromise promise = 1;
  Commitment commitment = 2;
  repeated RowWithProof rows = 3;                         // this FSP's assigned subset only
  RlcOrig rlc_orig = 4;                                   // corrected field number
}

message UploadRowsResponse {
  bytes validator_signature = 1;                          // ed25519 over SignBytes (see §2.3)
  optional uint64 ttl = 2;                                // seconds remaining (if known)
  optional uint32 backoff_ms = 10;                        // server hint for client backoff
}

message GetRowsRequest {
  Commitment commitment = 1;
}

message GetRowsResponse {
  repeated RowWithProof rows = 1;                         // ALL rows this FSP holds for the commitment
  RlcOrig rlc_orig = 2;
  optional uint64 ttl = 3;
  optional uint32 backoff_ms = 10;
}

service Fibre {
  rpc UploadRows(UploadRowsRequest) returns (UploadRowsResponse);
  rpc GetRows(GetRowsRequest) returns (GetRowsResponse);
}
```

### 1.2 FibreAccount API (control plane)

> Typed pass-through to the payments module. Message types/semantics are defined by the `x/fibre` spec.

```proto
service FibreAccount {
  rpc QueryEscrowAccount(QueryEscrowAccountRequest) returns (QueryEscrowAccountResponse);
  rpc Deposit(DepositRequest) returns (DepositResponse);
  rpc Withdraw(WithdrawRequest) returns (WithdrawResponse);
  rpc PendingWithdrawals(PendingWithdrawalsRequest) returns (PendingWithdrawalsResponse);
}
```

### 1.3 PaymentProcessor API (control plane)

> Server-side helpers to relay `MsgPayForFibre` and `MsgPaymentTimeout` (best-effort).

```proto
service PaymentProcessor {
  rpc SubmitPayForFibre(SubmitPFFRequest) returns (SubmitPFFResponse);
  rpc SubmitPaymentTimeout(SubmitPaymentTimeoutRequest) returns (SubmitPaymentTimeoutResponse);
}
```

---

## 2) Handler Semantics

### 2.1 `Fibre.UploadRows`

**Goal:** accept rows for a **specific FSP's** assignment slice, verify payment authorizations and proofs, store rows with TTL, and return the validator's signature over the PP.

**Flow (normative):**

1. **Validate stateless:**

    * msg size ≤ 8 MiB (enforced by max gRPC message size), TODO: update based on latest params
    * size bounds: `0 < blob_size ≤ 128 MiB` (derived from params),
    * row inclusion proof size matches `original_rows` size (network level params)
    * `signer` bech32, `namespace` version=2 and 29 bytes,
    * `commitment` 32 bytes,
    * `fibre_blob_version == 1`,
    * `creation_timestamp` ∈ `[now - withdrawal_delay - skew, now + skew]`,
    * `valset_height > 0`,
    * `signature` present.

2. **Proof/encoding checks:**

   * Verify RLC and Merkle proofs for provided `rows` against `commitment` and `rlc_orig`.
   * On failure: `INVALID_ARGUMENT` (ErrInvalidEncoding). **Note:** PP remains in unprocessed; timeout may still charge.

3. **Idempotency checks:**

    * **Put** PP found in **unprocessed** index. If found already → **OK** (return `validator_signature`)

4. **Stateful PP validation (payments module):**

    * Call `ValidatePaymentPromise`:

        * escrow exists,
        * sufficient **available** balance for gas bound,
        * signer signature valid,
        * not processed.
    * On failure: return `FAILED_PRECONDITION` (ErrInvalidPaymentPromise or ErrInsufficientBalance). With machine-readable detail code.

5. **Assignment check:**

    * Load `valset` at `valset_height`, run `Assignment(commitment, valset)`.
    * Ensure the `rows.index` set equals the assignment slice for **this** FSP.
    * On mismatch: `PERMISSION_DENIED` (ErrInvalidAssignment).

6. **Persist rows (single horizon):**

    * Store under `(commitment, valset_height)` with TTL=`retention_ttl` (24h),
    * **Multiple PPs for the same commitment:** store rows per PP (per `valset_height`).

7. **Return validator signature:**

    * Sign `SignBytes` (see §2.3) with validator key (ed25519),
    * Reply with `validator_signature`, and optional `ttl` / `backoff_ms`.

**gRPC status mapping (suggested):**

* `INVALID_ARGUMENT` — malformed fields, encoding/proofs invalid.
* `FAILED_PRECONDITION` — insufficient balance, PP not valid in current window.
* `PERMISSION_DENIED` — assignment slice mismatch.
* `ALREADY_EXISTS` — if client attempts to overwrite different rows for the same (commitment, valset\_height) and FSP slice.
* `RESOURCE_EXHAUSTED` — rate-limit/back-pressure (include `backoff_ms`).

### 2.2 `Fibre.GetRows`

1. Validate request (non-empty commitment).
2. Lookup **all rows this FSP holds** for `commitment` across all stored PPs.
3. If none: `NOT_FOUND` (ErrCommitmentNotFound).
4. Return `rows` + `rlc_orig` and `ttl`/`backoff_ms` hints.

### 2.3 Validator `SignBytes`

Validators sign the PP domain to attest service for the data:

```text
SignBytes = SHA256(
  "fibre/pp:v1" || Chain_id ||
  signer_bytes || namespace ||
  blob_size_u32be || commitment ||
  fibre_blob_version_u32be || creation_timestamp_pb || valset_height_u64be
)
```

* `signer_bytes`: 20-byte raw account address.
* `creation_timestamp_pb`: protobuf `Timestamp` bytes.
* Scheme: **ed25519** over the above digest.

---

## 3) Internal Architecture

### 3.1 Components (expanded)

1. **FibreServer**

    * Implements `Fibre` service.
    * Manages lifecycle, config, and subcomponents.
    * Delegates to subcomponents:
      * `StateAccessor`,
      * `ValidatorTracker`,
      * `ShardMap`,
      * `RowsStorage`,
      * `PromiseStore`,
      * `RateLimiter`.

2. **PromiseStore**

    * Two logical indexes:

        * **Unprocessed**: PPs seen but not finalized on-chain. Cleanup  via `MsgPayForFibre` or `MsgPaymentTimeout`.
    * Responsibilities:

        * Idempotency checks (first-seen vs. duplicates).
        * Promotes PPs from unprocessed → processed on-chain events.
        * Timeouts: scans unprocessed for expirations; submits `MsgPaymentTimeout` (best-effort).

3. **RowsStorage**

    * Persists `(commitment, valset_height)` buckets with:

        * Assigned rows + `rlc_orig`,
    * TTL GC based on `retention_ttl` (24h).
    * **Badger layout (v1):**

        * `d/<commitment>/<valset_height>` → rows blob
        * `b/<YYYYMMDDHHmm>/<commitment>/<valset_height>` → retention buckets

4. **ValidatorTracker**

    * Fetches historical validator sets by height (fast-path cache + light/RPC backend).
    * Exposes `Get(height) -> (valset, err)` and quorum checks (power, count).
    * Internally listens to events from StateAccessor to keep up-to-date.
    * Retains recent sets for performance (e.g., last 24h of heights). TODO: consider to lower it if memory usage is too high.

5. **ShardMap**

    * Deterministic assignment:

        * Permutes shares by `SHA256("share"||commitment||i)`,
        * Optionally permutes validators by `SHA256("validator"||commitment||j)`,
        * Splits into contiguous ranges per validator (base/r scheme).

6. **StateAccessor** (payments bridge)

    * Wraps queries to `x/fibre`:

        * `ValidatePaymentPromise(PP)`,
        * `QueryEscrowAccount`, `PendingWithdrawals`,
        * Relay helpers for `SubmitPayForFibre`, `SubmitPaymentTimeout`.
    * May return **proofs** (if enabled) for client verification.

7. **RateLimiter / Back-pressure**

<!-- markdownlint-disable MD031 -->
```text
* Token buckets **per peer** and **total throughput**,
* Global concurrency caps: `max_concurrent_uploads`, `max_concurrent_gets`,
* Emits `RESOURCE_EXHAUSTED` with `backoff_ms`.
```

1. **Telemetry**

```text
* Metrics: RPS, bytes/s, assignment mismatches, proof/RLC failures, write latency, GC times, validator_sig issuance, PP validation latency, insufficient-balance events, backoff distribution.
```
<!-- markdownlint-enable MD031 -->

### 3.2 Data Model & Keys

* **PP keys**
  * `pp/unprocessed/h/<promise_hash>` → PP metadata without user signature (signer, creation\_timestamp, valset\_height, gas\_bound, …)
  * `pp/unprocessed/b/<YYYYMMDDHHmm>/<promise_hash>` → TTL bucket index

* **Commitment data**
  * `d/<commitment>/<valset_height>` → rows blob, rlc\_orig
  * `b/<YYYYMMDDHHmm>/<commitment>/<valset_height>` → TTL bucket index

**Note:** `promise_hash = SHA256( PaymentPromise (canonical bytes) )`.

### 3.3 Background Workers

1. **BlockSubscriber**

    * Subscribes to app events (`EventPayForFibre`, etc.),
    * Moves matching entries from **unprocessed → processed**,
    * Updates local caches.

2. **TimeoutScanner**

    * Scans **unprocessed** for `creation_timestamp + promise_timeout ≤ now`,
    * Submits `MsgPaymentTimeout` (best-effort).

3. **TTLPruner**

    * Iterates `b/<bucket>` to prune expired `(commitment, valset_height)` data.

---

## 4) Configuration

```text
chain_id: string                     # domain for SignBytes
retention_ttl: 24h                   # single horizon
rows_per_message_limit: 166      # ≈ ceil(N / |valset|) + 1 (guard rail)
throughput_cap_bytes_per_sec: 50_485_760
payment_processing_timeout: 1h
max_concurrent_uploads: 100
max_concurrent_gets: 100
clock_skew_allowance: 5s           # TODO: finalize value
```

TODO: `rows_per_message_limit` should be derived at runtime from the **actual** `|valset|` in `promise.valset_height` to avoid false positives.

---

## 5) Rate-Limit & DoS Controls

* **Admission control**: check message sizes against `rows_per_message_limit` and effective `row_size`.
* **Per-client/total** token buckets with refill tied to `throughput_cap_bytes_per_sec`.
* **Burst caps**: bound outstanding inflight bytes and RPCs per peer.
* **Backoff hints**: include `backoff_ms` in responses; prefer exponential backoff with jitter on clients.
* **Server enforced keepalive policy**: close idle connections after a timeout (e.g., 2m).

---

## 6) Security & Correctness

* **Replay protection**: ChainID in SignBytes.
* **Namespace enforcement**: namespace **version=2** only.
* **Quorum thresholds**: server signs upon local PP validation; on-chain acceptance still requires ≥2/3 **power** and ≥2/3 **count** (client enforces before submitting PFF).
* **Clock skew**: small tolerance (`clock_skew_allowance`) for `creation_timestamp` checks.
* **Multiple PPs / same commitment**: store each valid PP independently (partition by `valset_height`); `GetRows` returns **all** rows the FSP holds for the commitment.

---

## 7) Errors

| Code                  | When                                                         |
| --------------------- | ------------------------------------------------------------ |
| `INVALID_ARGUMENT`    | Malformed PP fields; encoding/proof invalid; bad commitment. |
| `FAILED_PRECONDITION` | Insufficient escrow; PP not valid in window; state mismatch. |
| `PERMISSION_DENIED`   | Assignment slice mismatch (rows not belonging to this FSP).  |
| `NOT_FOUND`           | Commitment unknown to this FSP.                              |
| `ALREADY_EXISTS`      | Conflicting re-upload for same (commitment, valset\_height). |
| `RESOURCE_EXHAUSTED`  | Rate-limit / capacity exceeded; backoff advised.             |
| `UNAVAILABLE`         | Backend (node) RPC failure; try later.                       |
| `INTERNAL`            | Unhandled server error.                                      |

TODO: Add machine-readable error **details** (e.g., `{available_balance, required_payment}` on balance failures).
