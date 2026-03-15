# ADR 027: Single Sequencer BFT Ordering on Fibre

## Status

DRAFT

## Changelog

- 2026/03/07: Initial draft

## Context

Fibre already provides data availability: users post blobs and collect > 2/3 validator signatures as availability certificates. However, these signatures do not provide **ordering** — they attest that data is available, not that it occupies a specific position in a sequence.

Rollups that need BFT ordering today must wait for Celestia finality (~ 2 * block time). The rollup waits for the blob to be included in a Celestia block, then constructs a namespace inclusion proof against the Celestia header, parses the share from that proof, and compares the commitment. This adds seconds of latency, couples ordering to Celestia's block time, and requires proving inclusion through the Celestia header — an external dependency that adds latency, complexity, and proof overhead beyond what Fibre itself provides.

The key observation is that Fibre's existing validator signatures can be extended to cover ordering, not just availability. If validators enforce a **chaining rule** before signing — verifying that the previous rollup block has a quorum certificate, that the current block is the next sequential height, and that the current proposal signs a fixed-size hash of the previous ordered proposal — then collecting > 2/3 signatures becomes an ordering protocol. Each signature already covers the blob's commitment and namespace; the rule adds an authenticated link to the certified predecessor, turning an unordered set of availability attestations into a total order.

## Protocol Summary

This ADR extends Fibre's existing `PaymentPromise` signing flow into a chained BFT ordering protocol for a single sequencer. The key concepts:

- **Lane.** A `(chain_id, namespace, signer_public_key)` triple that defines an independent total order. Each signer has their own sequence within a namespace — no registry or namespace authorization is needed.
- **Sequence.** A monotonic position within a lane, starting at 1 (genesis). Sequence 0 means non-ordered (the proto default for `uint64`) and uses v0 sign bytes, identical to an existing PaymentPromise.
- **OrderedPaymentPromise.** A wrapper around `PaymentPromise` that adds a `sequence` number, a `previous_payment_promise_hash`, a `ParentPaymentPromise` descriptor (the parent's sign-bytes-relevant fields only), and the parent's quorum certificate. Uses v1 sign bytes (same fields as v0 but with a `fibre/pp:v1` prefix instead of `fibre/pp:v0`, and the sequence number and previous hash appended).
- **ParentPaymentPromise.** A stripped-down representation of the parent ordered promise containing only the fields needed to reconstruct the parent's v1 sign bytes and ordered hash for QC verification. Excludes the sequencer signature and excludes the parent's own witness data (`parent_promise`, `parent_signatures`), which would otherwise recurse.
- **Quorum Certificate (QC).** A set of > 2/3 voting power in validator ed25519 signatures over an `OrderedPaymentPromise`. Carried forward as proof of the parent's certification.

The protocol proceeds in four steps per sequence:

1. **Propose.** The sequencer constructs an `OrderedPaymentPromise` at sequence `s` containing `previous_payment_promise_hash = hash(B_{s-1})`, `ParentPaymentPromise_{s-1}`, and `QC_{s-1}` (or empty parent fields at genesis), signs it with v1 sign bytes, and uploads shards to validators.
2. **Vote.** Each validator verifies the parent QC, checks for double-voting, and signs the promise if all checks pass.
3. **Certify.** The sequencer collects > 2/3 voting power in signatures, forming `QC_s`. Block `B_s` is now **certified/locked**.
4. **Finalize.** When the sequencer posts `B_{s+1}` and obtains `QC_{s+1}`, block `B_s` is **finalized**. The second quorum on the child authenticates the exact parent via `previous_payment_promise_hash`, providing two-phase BFT confirmation.

Finality requires two QC rounds — one to lock, one to finalize — matching the minimal two-phase structure of standard BFT protocols. The rest of this document specifies each component in detail.

## Specification

### Model

The Fibre validator set follows the standard weighted BFT model: `n = 3f + 1`, or equivalently, a quorum requires `> 2/3` of voting power. Liveness requires `> 2/3` of voting power to be online for any QC to form.

There is no namespace authorization mechanism — any signer may start a new chain in any namespace. Lane isolation comes from the signer's public key being part of the lane key and the v1 sign bytes: different signers produce different sign bytes even at the same sequence in the same namespace. The chaining rule (same signer in parent and child) ensures each signer's chain is independent. Authored namespaces could further restrict who may write to a namespace, but this is orthogonal to the ordering protocol.

The `(chain_id, namespace, signer_public_key)` triple defines a total order of posted blobs. The position in this order is the **sequence** `s`. Ordering starts at sequence 1; sequence 0 is reserved for non-ordered promises (the proto default for `uint64`).

### Protocol

**Step 1: Propose.** The sequencer constructs an `OrderedPaymentPromise` for sequence `s` with `previous_payment_promise_hash` set to the ordered hash of sequence `s-1`, `parent_promise` set to the serialized `ParentPaymentPromise` from sequence `s-1`, and `parent_signatures` set to the validator signatures from `QC_{s-1}` (or all parent fields empty if `s == 1`). It signs the promise with v1 sign bytes, encodes the rollup block as a Fibre blob, and uploads shards to validators via `UploadShard`.

**Step 2: Vote.** Each validator runs the ordering `PromiseRule` ([ADR-025](adr-025-promise-rules.md)). If it passes (along with all existing Fibre verification: promise validation, shard assignment, proof verification), the validator signs the `OrderedPaymentPromise` via `SignOrderedPaymentPromiseValidator` and returns the signature.

**Step 3: Certify.** The sequencer collects `> 2/3` voting power in validator signatures, forming `QC_s`. This is the first-phase certificate — `B_s` is now **certified/locked**.

**Step 4: Finalize.** When the sequencer posts `B_{s+1}` with `previous_payment_promise_hash = hash(B_s)`, includes `QC_s` as witness data, and obtains `QC_{s+1}`, then `B_s` is **finalized**. The second quorum on the child block provides the second phase of confirmation.

### Safety Rules

1. **No double vote** — A validator MUST NOT sign two different ordered-promise hashes for the same `(chain_id, namespace, signer_public_key, sequence)`. Signing the same ordered-promise hash is permitted (idempotent).
2. **Chain extension** — A validator signs sequence `s` only if the proposal includes a valid `QC_{s-1}` (or `s == 1`) and that QC certifies the exact parent whose ordered hash equals `previous_payment_promise_hash`.
3. **Commit rule** — `B_s` is **certified/locked** when `QC_s` exists, and **finalized** when `QC_{s+1}` exists for a child whose `previous_payment_promise_hash = hash(B_s)`. A single QC is unique (no conflicting QC can form under the no-double-vote rule), but may not have been observed by enough validators to survive sequencer failure. The second QC on the child proves that `> 2/3` of validators observed the lock on that exact parent, ensuring the chain can continue after sequencer failure.

### OrderedPaymentPromise

A new Go wrapper type extends `PaymentPromise` with ordering fields and provides its own `SignBytes()` method that produces v1 sign bytes. The existing `PaymentPromise` and its v0 `SignBytes()` are unchanged.

```go
const orderedSignBytesPrefix = "fibre/pp:v1"

// OrderedPaymentPromise wraps a PaymentPromise with ordering fields.
// It provides its own SignBytes(), SignBytesValidator(), and HashOrdered()
// helpers for the chained v1 protocol.
type OrderedPaymentPromise struct {
    *PaymentPromise

    // Sequence is the monotonic ordering position. Starts at 1 (genesis).
    Sequence uint64

    // PreviousPaymentPromiseHash is the ordered hash of the parent
    // proposal at sequence s-1. Nil for genesis.
    PreviousPaymentPromiseHash []byte

    // ParentPromise contains the sign-bytes-relevant fields of the
    // ordered promise from the previous sequence (s-1), deserialized
    // from the parent_promise bytes on the wire. Nil for genesis
    // (sequence == 1).
    ParentPromise *ParentPaymentPromise

    // ParentSignatures are the validator ed25519 signatures over
    // ParentPromise, forming QC_{s-1}. Nil for genesis.
    // Positionally ordered: index i corresponds to validator i in the
    // validator set at the parent's Height. Non-signing validators
    // have nil entries.
    ParentSignatures [][]byte
}

// SignBytes returns v1 sign bytes: the same fields as v0 but with
// a "fibre/pp:v1" prefix and the sequence number plus the previous
// ordered-promise hash appended.
//
// Format: "fibre/pp:v1" || chainID || signerPubKey(33) || namespace(29) ||
//         blobSize(4) || commitment(32) || blobVersion(4) || height(8) ||
//         creationTimestamp(15) || sequence(8) ||
//         previousPaymentPromiseHash(32 or empty)
func (o *OrderedPaymentPromise) SignBytes() ([]byte, error) {
    // ... v1 computation ...
}

// HashOrdered returns the canonical ordered hash for chaining.
// It hashes the consensus object derived from v1 sign bytes, not the
// recursively nested wire payload.
func (o *OrderedPaymentPromise) HashOrdered() ([]byte, error) {
    // ... v1 ordered hash computation ...
}

// SignBytesValidator returns the v1 sign bytes wrapped with CometBFT
// domain separation using the v1 prefix.
func (o *OrderedPaymentPromise) SignBytesValidator() ([]byte, error) {
    signBytes, err := o.SignBytes()
    if err != nil {
        return nil, err
    }
    return core.RawBytesMessageSignBytes(o.ChainID, orderedSignBytesPrefix, signBytes)
}

// SignOrderedPaymentPromiseValidator signs the OrderedPaymentPromise
// using the validator's private key with the v1 prefix for CometBFT
// domain separation.
func SignOrderedPaymentPromiseValidator(
    promise *OrderedPaymentPromise,
    privVal core.PrivValidator,
) ([]byte, error) {
    signBytes, err := promise.SignBytes()
    if err != nil {
        return nil, err
    }
    return privVal.SignRawBytes(
        promise.ChainID, orderedSignBytesPrefix, signBytes,
    )
}
```

This design provides a clean type-level separation: code handling `*PaymentPromise` uses v0 sign bytes, code handling `*OrderedPaymentPromise` uses v1. The existing `PaymentPromise` is not modified.

The sequencer client constructs an `OrderedPaymentPromise` when posting ordered blobs and signs with v1 sign bytes. The ordering `PromiseRule` constructs the wrapper on the server side to verify the sequencer's signature and to produce the correct validator sign bytes.

`OrderedPaymentPromise` is a Go wrapper type, not a wire type. What travels over the wire is the existing `PaymentPromise` proto message with four new optional fields (see [Proto Extensions](#proto-extensions)). Both the sequencer client and the validator server construct the `OrderedPaymentPromise` wrapper from those parsed proto fields — the sequencer to sign with v1 sign bytes, the validator to verify the sequencer's signature and produce its own.

**On the wire**, the ordering fields (`sequence`, `previous_payment_promise_hash`, `parent_promise`, `parent_signatures`) are carried as proto fields on the `PaymentPromise` message. The `parent_promise` field contains a serialized `ParentPaymentPromise` descriptor — a stripped-down message with only the fields needed for sign-bytes and ordered-hash reconstruction, not a full `PaymentPromise`.

**On the server**, `FromProto()` populates the `PaymentPromise` Go struct from the proto message as it does today. The ordering `PromiseRule` then deserializes the `ParentPaymentPromise` from the `parent_promise` bytes, constructs the `OrderedPaymentPromise` wrapper from the parsed fields, and uses it for v1 sign-bytes and ordered-hash computation — verifying the sequencer's signature, verifying that the parent QC certifies the exact referenced parent hash, and producing the validator's own signature.


### Ordered Hash

The ordered hash is a distinct v1 chaining object. It is not the existing v0 `PaymentPromise.Hash()`, and it is not a hash of the full nested protobuf payload.

For an `OrderedPaymentPromise`, define:

```text
ordered_payment_promise_hash_v1 =
  H("fibre/pp:v1/hash" || v1_sign_bytes)
```

Where `v1_sign_bytes` are the bytes returned by `OrderedPaymentPromise.SignBytes()`. This keeps the chained reference fixed-size and non-recursive: the child refers only to the already-computed parent hash, while `parent_promise` and `parent_signatures` remain witness data used to verify that hash.

### Proto Extensions

A new `ParentPaymentPromise` message contains only the fields needed to reconstruct the parent's v1 sign bytes and ordered hash — the minimum required for a validator to verify `QC_{s-1}` and bind it to `previous_payment_promise_hash`. It deliberately excludes:

- **`signature`** (the sequencer's secp256k1 signature) — redundant because the parent QC already proves >2/3 of validators verified the sequencer's signature when they signed `QC_{s-1}`.
- **Witness fields** (`parent_promise`, `parent_signatures`) — including these would cause recursive nesting, growing message size linearly with chain height. The descriptor includes the parent's own `sequence` and `previous_payment_promise_hash`, but never its ancestor witness data.

```protobuf
// ParentPaymentPromise is a stripped-down PaymentPromise containing
// only the fields needed to reconstruct v1 sign bytes and the ordered
// hash for QC verification. Validators deserialize this, recompute
// sign bytes and hash from the parsed fields, and verify
// parent_signatures against those recomputed sign bytes — never
// against attacker-supplied bytes.
message ParentPaymentPromise {
  string chain_id       = 1;
  bytes  signer_public_key = 2;
  bytes  namespace       = 3;
  uint32 blob_size       = 4;
  bytes  commitment      = 5;
  uint32 blob_version    = 6;
  int64  height          = 7;
  google.protobuf.Timestamp creation_timestamp = 8;
  uint64 sequence        = 9;
  bytes  previous_payment_promise_hash = 10;
}
```

Four new optional fields are added to `PaymentPromise`:

```protobuf
message PaymentPromise {
  // ... existing fields 1-9 ...

  // sequence is the monotonic ordering position for this lane.
  // Zero means non-ordered (the proto default). Ordering starts at 1.
  uint64 sequence = 10;

  // previous_payment_promise_hash is the ordered hash of the parent
  // proposal at sequence s-1.
  //
  // Present and 32 bytes when sequence > 1. Empty for genesis
  // (sequence == 1) and non-ordered (sequence == 0).
  bytes previous_payment_promise_hash = 11;

  // parent_promise is the serialized ParentPaymentPromise from
  // sequence s-1. Contains only the fields needed to reconstruct
  // the parent's v1 sign bytes and ordered hash for QC verification.
  // Present when sequence > 1. Empty for genesis (sequence == 1)
  // and non-ordered (sequence == 0).
  bytes parent_promise = 12;

  // parent_signatures are the validator ed25519 signatures over the
  // parent's v1 sign bytes (reconstructed from parent_promise),
  // forming QC_{s-1}.
  //
  // Encoding: positionally ordered by the validator set at the parent
  // promise's height. Entry i corresponds to validator i in that set.
  // Non-signing validators have empty bytes (length 0). Signing
  // validators have exactly 64 bytes (ed25519 signature). The length
  // of this list equals the validator set size at the parent's height.
  //
  // This matches the existing SignedPaymentPromise.ValidatorSignatures
  // encoding used by the Fibre client.
  //
  // Present when sequence > 1. Empty for genesis and non-ordered.
  repeated bytes parent_signatures = 13;
}
```

This is backwards-compatible: old validators and clients ignore unknown fields. Non-ordered blobs leave all four fields at their zero values, behaving identically to today. However, all validators must understand and enforce the ordering rule before sequencers can rely on ordered promises — a coordinated upgrade is required.

Including `parent_promise` (a serialized `ParentPaymentPromise`) alongside `parent_signatures` makes the QC self-contained: any validator can verify the parent QC without needing to have seen or stored the parent promise previously. The validator deserializes the `ParentPaymentPromise`, recomputes the parent's v1 sign bytes and ordered hash from its fields, verifies `parent_signatures` against those recomputed sign bytes, and checks that the resulting ordered hash exactly equals the child's `previous_payment_promise_hash`. This is what enables validators to catch up after missing sequences.

The v1 sign bytes include the `sequence` and `previous_payment_promise_hash` to prevent signature replay across sequences and to authenticate the parent edge. All integer fields use big-endian encoding. `creationTimestamp` is encoded via `time.Time.MarshalBinary()` (15 bytes). Sequence 0 never appears in v1 sign bytes — non-ordered promises use v0. The ordered hash used for chaining is defined over the v1 consensus object, not over recursively nested protobuf bytes, so message size stays constant per sequence apart from the carried QC witness data.

### Genesis

Sequence `s = 1` is the genesis block. It is a special case:

- `previous_payment_promise_hash` is empty or all-zero
- `parent_promise` is empty (no previous sequence to certify)
- `parent_signatures` is empty
- Validators accept empty parent data only when `sequence == 1`

The sequencer posts a blob with `sequence = 1` and empty parent fields. Validators sign it using v1 sign bytes if all other checks pass (no double vote). The resulting `QC_1` becomes the anchor for the chain — the signer who obtained `QC_1` is established as the lane's sequencer.

### OrderedPromise Validity Rules

On receiving a `PaymentPromise` with `sequence > 0`, the ordering `PromiseRule` ([ADR-025](adr-025-promise-rules.md)) constructs an `OrderedPaymentPromise` wrapper and verifies:

1. **Sequencer signature** — Verify the sequencer's secp256k1 `signature` against the v1 `SignBytes()` of the `OrderedPaymentPromise`, including `previous_payment_promise_hash`. *Ensures the sequencer authorized this specific ordered promise and this exact parent edge.*

2. **Lane continuity** — At genesis (`sequence == 1`), any signer may start a new chain. For `sequence > 1`, verify that the `parent_promise` fields `chain_id`, `namespace`, and `signer_public_key` all match the corresponding fields of the current proposal. *Ensures each lane's chain is independent and prevents cross-lane parent QC reuse.*

3. **Genesis or valid parent QC** — If `sequence == 1`, `previous_payment_promise_hash`, `parent_promise`, and `parent_signatures` must be empty (or zero for the hash). If `sequence > 1`, `previous_payment_promise_hash` must be present and 32 bytes. Deserialize the `ParentPaymentPromise` from `parent_promise`, verify `parent.sequence == current.sequence - 1`, recompute the parent's v1 sign bytes and ordered hash, verify that `parent_signatures` contain `> 2/3` voting power in valid ed25519 signatures over those recomputed sign bytes, checked against the validator set at the parent's `height`, and verify that the recomputed parent ordered hash exactly equals `previous_payment_promise_hash`. The parent QC is self-contained: validators that missed previous sequences can catch up without prior local state.

4. **Existing Fibre checks** — Commitment correctness and data availability checks in the Fibre shard verification pipeline apply unchanged. However, the existing `Validate()` sequencer signature check (which verifies against v0 `SignBytes()`) must be skipped or adapted for ordered promises (`sequence > 0`), since the ordering PromiseRule handles signature verification with v1 sign bytes.

5. **No double vote** — The validator has not already signed a different ordered-promise hash at the same `(chain_id, namespace, signer_public_key, sequence)`. Signing the **same** ordered-promise hash is permitted (idempotent) to support sequencer crash recovery. Stale proposals at sequences ≤ the lane watermark are implicitly rejected — see [Local Validator State](#local-validator-state).

If all checks pass, the validator signs the `OrderedPaymentPromise` via `SignOrderedPaymentPromiseValidator` and returns the signature in the `UploadShardResponse`.

### Local Validator State

To enforce the no-double-vote and chain-extension rules, validators must durably track two pieces of state per ordered lane `(chain_id, namespace, signer_public_key)`:

**1. Lane watermark** — the highest certified sequence the validator has adopted. This is the sequence from the most recently verified parent QC, not the highest sequence the validator has personally signed.

- Initialized when the validator adopts `QC_1` (genesis)
- Advanced when verifying a valid parent QC: set to `max(current_watermark, parent_sequence)`
- Never decremented
- Allows validators to catch up after missing sequences without replaying the full chain locally

**2. Per-sequence signed ordered-promise hash** — for each `(chain_id, namespace, signer_public_key, sequence)`, the ordered-promise hash the validator signed, if any.

- Enforces the no-double-vote rule
- Old entries can be pruned once the watermark advances past them, since the watermark check will reject stale proposals for earlier sequences

#### Durability

This state is **safety-critical**: if a validator loses its double-vote record and re-signs a different ordered-promise hash at the same sequence after a restart, it can contribute to a safety violation. The state must survive crashes.

This state must be crash-safe (e.g., WAL or embedded KV store). The storage requirement is minimal: one watermark per active lane, plus one ordered-promise hash per unpruned sequence.

All validators must be responsible for their own state backups and restores.

#### Pruning and Resource Pricing

Pruning policy and resource pricing for per-lane validator state fall outside the scope of this document, as there are several viable options. The key invariant is that a validator MUST NOT prune signed ordered-promise hash records for any sequence where equivocation could still cause a safety violation.

## Alternative Approaches

### A. No ordering from fibre signatures

Rollups continue using Celestia block finality for ordering. The sequencer posts blobs via Fibre for availability, but ordering comes from Celestia headers with namespace inclusion proofs. This is the status quo and is already functional. The cost is higher latency (~6s for Celestia finality) and more complex verification (inclusion proofs, share parsing).

### B. Future extensions

The following are not alternatives to this design but extensions that build on top of it:

- **Multi-proposer ordering (fixed set)**: Multiple known proposers take turns or rotate leadership via a view-change protocol.
- **Multi-proposer ordering (unknown proposers)**: Proposers are not known in advance. Further extends the multi-proposer model.
- **Time-window signing restrictions**: Validators refuse to sign promises whose `CreationTimestamp` falls outside a configurable window from the validator's local clock. Limits the window for equivocation and stale proposals. Complementary to ordering, not a replacement.
- **Single-shot finality via > 4/5 quorum**: Under a stronger fault model (`n = 5f + 1`, at most 1/5 Byzantine — compared to the `n = 3f + 1` model used in the rest of this document), a higher quorum threshold enables finalization in one round instead of two. Two > 4/5 quorums overlap by at least 3/5; since at most 1/5 can be Byzantine, the overlap contains at least 2/5 honest validators, sufficient to rule out conflicting locks. The protocol shape is identical; only the fault model, quorum size, and confirmation rule change. Liveness requires > 4/5 online.

## Decision

TBD, however this proposal is suggesting:

Implement a minimal single-proposer chained BFT ordering protocol for Fibre. The protocol:

- Introduces an `OrderedPaymentPromise` wrapper type with v1 sign bytes that include the sequence number and `previous_payment_promise_hash`
- Enforces ordering via a `PromiseRule` ([ADR-025](adr-025-promise-rules.md)), composable with other rules
- Uses two-phase finalization with `> 2/3` voting power (lock at `QC_s`, finalize at `QC_{s+1}`)
- Requires durable validator state (WAL or embedded KV store) for double-vote protection

The existing `PaymentPromise` type and v0 sign bytes are unchanged. Non-ordered blobs continue to work identically.

## Consequences

### Positive

- Full BFT finality for single-sequencer rollups with two-phase `> 2/3` confirmation.
- Portable, self-contained proofs — no dependency on Celestia block headers or namespace inclusion proofs.
- Authenticated parent-child chaining: `QC_{s+1}` finalizes the exact parent referenced by `previous_payment_promise_hash`.
- Composable `PromiseRule` ([ADR-025](adr-025-promise-rules.md)) — minimal changes to Fibre server/client.
- Non-breaking: non-ordered blobs are unchanged.

### Neutral
- Adds opportunities for more protocol revenue, since ordering adds costs
- Coordinated validator upgrade required for v1 sign bytes.

### Negative

- Validators must durably track per-lane double-vote state.
- While resource pricing should cover costs, ordered promises carry additional witness data: `previous_payment_promise_hash` (32 bytes), `parent_promise` (`ParentPaymentPromise`), and `parent_signatures` (~64 bytes per signing validator).

## References

- [ADR-025](adr-025-promise-rules.md) — Promise Rules for Fibre
- `fibre/payment_promise.go` — `PaymentPromise` type, `SignBytes()`, `SignPaymentPromiseValidator()`
- `fibre/blob_id.go` — `Commitment` type
- `proto/celestia/fibre/v1/fibre.proto` — `PaymentPromise` proto definition
- `proto/celestia/fibre/v1/service.proto` — `UploadShardRequest` / `UploadShardResponse`
