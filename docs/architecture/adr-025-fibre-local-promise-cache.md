# ADR 025: Fibre Local Promise Cache

## Changelog

- 2026-03-27: Initial draft
- 2026-03-29: Add nonce-based option (Option B)

## Status

Proposed

## Context

When a validator receives a blob upload via fibre, it queries the app's `ValidatePaymentPromise` endpoint to verify that the signer's escrow account has sufficient available balance. This check reads the chain state at query time. Between validation and on-chain settlement (via `MsgPayForFibre` or `MsgPaymentPromiseTimeout`), the balance is not reserved. Two concurrent promises from the same signer can both pass validation against the same available balance, and the second one succeeds on-chain only if enough balance remains after the first settles.

This is a double-spend window at the validator level. A signer with 100 utia available can submit two 80 utia promises concurrently. Both pass validation. The first settles on-chain. The second either fails on-chain (wasting validator resources) or succeeds if it arrives before the first is processed. The validator has no local state to detect the conflict.

## Prerequisites

- [celestiaorg/celestia-app#6898](https://github.com/celestiaorg/celestia-app/pull/6898). This ADR assumes #6898 is merged. That PR allows payments to reduce pending withdrawals when `AvailableBalance` is insufficient. Withdrawals take 24 hours to execute. If a user initiates a withdrawal and then continues sending payment promises, the payments are deducted from the pending withdrawal amounts (oldest first). The user should avoid sending payment promises after initiating a withdrawal, but if they do, the withdrawal amount is reduced rather than the payment failing.

## Decision

Two options are presented. Both add a validator-local sidecar cache that tracks per-signer budget and pending promise reservations. The cache is used only by the `ValidatePaymentPromise` query path. Consensus execution in `msg_server.go` remains unchanged.

- **Option A** is protocol non-breaking. It uses periodic sweeps against chain state to reconcile the cache.
- **Option B** is protocol breaking. It adds a per-signer nonce to payment promises, allowing the cache to enforce ordering and avoid sweeps.

## Detailed Design

### Shared Design

The following sections apply to both options.

#### Cache Location and Storage

A new component `local_promise_cache.go` in `x/fibre/keeper/` backed by the node's underlying DB under a dedicated prefixed namespace, outside IAVL. It is injected into the Fibre keeper at app wiring time as a non-consensus dependency. If nil (tests, non-validator nodes), the keeper falls back to current behavior.

Two record types are persisted:

**SignerBudget** — One entry per signer. Tracks the budget state for a single escrow account.

- Key: `signer_budget/{signer}`
- Fields:
  - `last_known_balance` — The `AvailableBalance` read from chain state during the last sweep. The budget is computed relative to this value.
  - `available_budget` — Remaining budget for new promises: `last_known_balance - sum(pending promise amounts)`. Decremented on each reservation, reset on sweep.
  - `last_sweep_at` — Timestamp of the last sweep for this signer.
  - `ops_since_sweep` — Number of reservations since the last sweep. Used to determine staleness.

**PendingPromise** — One entry per accepted promise. Tracks a reservation that has not yet settled on-chain.

- Key: `pending_promise/{promise_hash}`
- Fields:
  - `signer` — The escrow account signer address.
  - `amount` — The reserved payment amount.
  - `created_at` — When the reservation was made.
  - `expires_at` — When the promise expires (used for eviction).

**Signer-to-promises index** — A secondary index to look up all pending promises for a signer without scanning all entries.

- Key: `signer_promises/{signer}/{promise_hash}` → empty value
- Used during sweeps to list a signer's unresolved promises.

Updates to `SignerBudget`, `PendingPromise`, and the index within a single reservation are written as an atomic DB batch.

#### Cache Eviction

If a signer has no new operations for longer than `PaymentPromiseTimeout + 1h` (i.e., all pending promises have either settled or expired and the timeout agent has had time to submit them), delete the signer's entire cache entry (SignerBudget and all PendingPromise records). This keeps the cache bounded to active signers only. The check is performed during reconciliation: if after dropping resolved/expired promises no pending promises remain and `now - last_reconcile_at` exceeds the eviction threshold, the signer is evicted. On the next validation for that signer, the cache is rebuilt from the chain state.

#### Restart Behavior

On startup, the cache is loaded from DB. Entries older than the eviction threshold (`PaymentPromiseTimeout + 1h`) are deleted immediately — these signers are inactive, and their pending promises have either settled or expired. Remaining entries are kept as-is; if stale, they are reconciled on that signer's next validation. No chain history rescan is performed.

#### Cache DB Failure

The cache shares the same DB as the application state. If the cache DB is corrupted, the application DB is also corrupted, and the validator will not start. There is no separate failure mode for the cache alone.

If the DB is reset (e.g., restoring from a snapshot without cache data), the cache starts empty. Double-spend protection is lost until the cache is rebuilt through incoming validations and sweeps.

#### Concurrency

Validation is guarded per-signer with a mutex so two concurrent promises for the same signer cannot both consume the same remaining budget. Reservations are idempotent by `promise_hash`.

The cache reservation happens in the app query path (`ValidatePaymentPromise`), but shard verification, storage, and validator signing happen afterward in the fibre server. The app has no callback if a later step fails. This means orphaned reservations can occur from operational issues such as the fibre server crashing after validation but before signing completes. These orphaned entries are cleaned up automatically: reconciliation drops promises that are already processed or expired, and cache eviction removes the entire signer entry after `PaymentPromiseTimeout + 1h`.

### Option A: Sweep-Based Cache (Protocol Non-Breaking)

#### Validation on the Query Path

`ValidatePaymentPromise` calls, in order:

1. Chain-only stateful checks (existing `ValidatePaymentPromiseStateful` logic, refactored to also return signer address, required amount, and current available balance).
2. Local cache budget check and reservation.

The local budget check:

1. Compute `promise_hash`. If a PendingPromise with that hash already exists, return success idempotently without decrementing budget again.
2. Load the signer's `SignerBudget`. If none exists, force a sweep.
3. If the cache is stale (older than 1 hour and at least one operation has occurred since the last sweep), force a sweep.
4. If `required_amount <= remaining_budget`, reserve: decrement `remaining_budget`, increment `ops_since_sweep`, write `PendingPromise` and updated `SignerBudget`.
5. If the budget is not enough, perform a sweep-and-retry. If it still does not fit, reject with insufficient balance.

#### Sweep Algorithm

A sweep is scoped to a single signer and rebuilds budget from fresh chain state:

1. Read current escrow `AvailableBalance` from chain state.
2. Load all locally pending promises for the signer.
3. Drop any pending promise that is already processed on-chain (via `IsPaymentPromiseProcessed`) or is no longer chargeable (outside the withdrawal-delay window).
4. Recompute: `remaining_budget = max(0, AvailableBalance - sum(kept promise amounts))`.
5. Reset `last_sweep_at = now`, `ops_since_sweep = 0`.
6. Persist compacted state and delete dropped promise records.

Withdrawals do not need special handling. Withdrawals are not immediate — they have a 24-hour delay between request and execution. During this delay, the withdrawn amount is already subtracted from `AvailableBalance` on-chain. Since sweeps read the current `AvailableBalance`, any pending or processed withdrawal is always reflected before it takes effect. An hourly sweep cadence is well within the 24-hour withdrawal window.

`GasPerBlobByte` can change via governance. During a sweep, pending promise amounts are recomputed using the current params, so a parameter update is reflected within the next sweep cycle.

#### Tradeoffs

**Single-process cache.** The cache is local to a single process. In sentry setups with multiple validator nodes, or deployments with multiple fibre servers, each instance maintains its own cache. A client can submit different promises to different instances of the same validator, bypassing the per-instance budget. A standalone shared cache would solve this but is out of scope for this iteration.

**Cache poisoning via exposed gRPC endpoint.** The cache is updated through the `ValidatePaymentPromise` gRPC query. If the endpoint is exposed, a malicious user could submit crafted promises to drain any signer's cached budget to zero, forcing more frequent sweeps and state reads. Requiring stateless validation (signature verification) before updating the cache mitigates this — the attacker would need access to the signer's private key to produce a valid promise.

**Frontrunning.** A malicious user who intercepts a legitimately signed promise could submit it directly to the validator's gRPC endpoint before the real client's fibre upload reaches the server. However when the client subsequently submits the same promise to the fibre server, the server can still accept and start serving the data. The cache is idempotent by `promise_hash` — the same promise is not double-counted in the budget.

**Sweep amplification from zero-balance accounts.** A malicious user could repeatedly submit promises from escrow accounts with zero or insufficient balance, forcing the cache to sweep on every request (since budget check fails and triggers a sweep-and-retry). As a follow-up, the cache should rate-limit sweeps for signers that fail with zero or insufficient balance — only re-sweeping at most once per block for such accounts.

**Selective-validator attack.** Clients can send different promises to different validators, skewing per-validator caches and enabling double spending — especially since the timeout agent can submit any promise with a single validator signature. The cache has no cross-validator visibility. This can be fixed by either Option B (nonce-based ordering) or by combining Option A with cross-validator Listen — see below.

#### Mitigating with Cross-Validator Listen

The selective-validator attack succeeds because each validator's cache is isolated — no validator knows what promises other validators have accepted. The [Listen](https://github.com/celestiaorg/celestia-app/issues/6806) method provides a way to close this gap without protocol changes.

When a validator accepts a payment promise and signs it, it broadcasts a notification to all other validators containing the promise hash, signer, and amount, signed with the validator's own key. Receiving validators verify the signature and deduct the amount from the signer's cached budget — the same operation as a local reservation, but triggered by a peer notification instead of a client request.

This works because:

- **Visibility.** If a client sends promise A to validator 1 and promise B to validator 2, both validators learn about each other's promise via the broadcast. Their caches reflect the combined budget impact.
- **Authentication.** Notifications are signed with the validator's key, preventing spoofed budget drains from external parties.
- **No protocol changes.** The broadcast is between validators at the fibre server layer. The PaymentPromise format, on-chain execution, and consensus rules are unchanged.

The tradeoff is additional communication overhead — each accepted promise triggers n-1 notifications across the validator set, on top of the normal fibre upload. Also, we would need to define an extra gRPC method in the querier to update the cache with the latest hashes signed by the other fibre servers.

**Consistency model.** The Listen mechanism provides eventual consistency. A validator's cache reflects the union of its own reservations plus whatever notifications it has received. There is no global lock or ordering guarantee — two validators may briefly have different budget views for the same signer. This is acceptable because the cache is an optimistic local filter, not a consensus mechanism. The chain remains the source of truth and rejects any promise that exceeds the actual on-chain balance at settlement time.

**Offline validators.** If a validator is offline, it misses notifications. When it comes back, its cache is stale — it only knows about its own reservations. The next sweep reconciles with chain state, picking up any promises that settled while it was offline. Between restart and the first sweep, the validator may over-accept promises for a signer whose budget was consumed by other validators. The sweep interval (1 hour or on insufficient balance) bounds this window.

**Out-of-order notifications.** Notifications are idempotent by `promise_hash` — the same reservation is applied at most once regardless of arrival order. There is no sequencing requirement. A notification for promise B arriving before promise A is harmless; both simply decrement the signer's budget independently.

**Conflicting budget views.** Two validators may temporarily disagree on a signer's remaining budget. This is resolved by sweeps: when a validator sweeps, it reads the actual `AvailableBalance` from chain state and recomputes the budget from scratch, converging to the same view as every other validator that sweeps. Between sweeps, over-reservation is possible but bounded by the minimum escrow bond.

#### Related Improvements

- Sweeps read directly from app state, which can block gRPC requests during cache re-seeding. Implementing read-only state snapshots for gRPC queries ([celestiaorg/cosmos-sdk#728](https://github.com/celestiaorg/cosmos-sdk/issues/728)) would avoid contention between sweep reads and consensus writes.

### Option B: Nonce-Based Cache (Protocol Breaking)

#### Protocol Changes

This option modifies the PaymentPromise format and on-chain escrow state:

- **PaymentPromise nonce field.** Each payment promise includes a per-signer incrementing nonce. The nonce is part of the signed bytes.
- **On-chain nonce tracking.** The escrow account stores the last settled nonce. Nonces must be processed sequentially on-chain — a promise with nonce N is only valid if nonce N-1 has been settled.

#### Validation on the Query Path

`ValidatePaymentPromise` calls, in order:

1. Chain-only stateful checks (same as Option A).
2. Local cache nonce and budget check.

The local nonce and budget check:

1. Compute `promise_hash`. If a PendingPromise with that hash already exists, return success idempotently without decrementing budget again.
2. Load the signer's cache. If none exists, read on-chain last settled nonce and `AvailableBalance` to initialize.
3. If `promise.nonce != last_known_nonce + 1`, reject. The response includes `last_known_nonce` so the client knows which promises are missing.
4. **Client catch-up:** to submit nonce N, the client must first send all missing promises (nonces between `last_known_nonce + 1` and N-1) to this validator. The validator tracks them for budget accounting.
5. If `required_amount <= remaining_budget`, reserve: decrement `remaining_budget`, store PendingPromise with nonce, advance `last_known_nonce`.

#### Budget Recovery

On insufficient balance, the cache reconciles with chain state:

1. Read the on-chain last settled nonce and current `AvailableBalance`.
2. Free all local promises with nonce <= last settled nonce.
3. Recompute: `remaining_budget = max(0, AvailableBalance - sum(unsettled promise amounts))`.

No per-promise `IsPaymentPromiseProcessed` calls are needed — the on-chain nonce is sufficient to determine which promises have settled.

`GasPerBlobByte` governance changes are picked up during budget recovery since `AvailableBalance` is re-read from chain state.

#### Tradeoffs

**Single-process cache.** Same as Option A.

**Cache poisoning via frontrunning.** Same as Option A.

**Ordered submission.** Nonces must be sequential on-chain. This constrains the order in which payment promises can be settled and prevents parallel settlement of independent promises from the same signer.

**Client catch-up logic.** Clients must track which validators have seen which nonces and send missing promises when switching or adding validators. This adds complexity to the client implementation.

## Consequences

### Option A

**Positive:**

- Closes the double-spend window at the validator level.
- No protocol changes — PaymentPromise format, client signing flow, and on-chain execution paths are unchanged.
- Preserves reservations across validator restarts without chain rescan.

**Negative:**

- Per-signer mutex serializes concurrent validations for the same signer. This is intended behavior to prevent oversubscription.
- Sweeps issue additional read requests against chain state (escrow balance, `IsPaymentPromiseProcessed` per pending promise) which increases query-path load on the state store.

### Option B

**Positive:**

- Closes the double-spend window at the validator level.
- Cheaper budget recovery: single on-chain nonce read vs. per-promise `IsPaymentPromiseProcessed` calls.
- Preserves reservations across validator restarts without chain rescan.

**Negative:**

- Per-signer mutex serializes concurrent validations for the same signer. This is intended behavior to prevent oversubscription.
- Protocol-breaking change — new nonce field in PaymentPromise sign bytes and protobuf definitions, on-chain nonce tracking in escrow account state.
- Requires ordered on-chain settlement, preventing parallel settlement of independent promises from the same signer.
- Adds client-side complexity for tracking and catching up validators with missing nonces.
