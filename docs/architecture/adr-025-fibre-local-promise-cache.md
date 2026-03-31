# ADR 025: Fibre Local Promise Cache

## Changelog

- 2026-03-27: Initial draft
- 2026-03-29: Add nonce-based option (Option B)
- 2026-03-31: Remove Option B, rename Option C to Option B

## Status

Proposed

## Context

When a validator receives a blob upload via fibre, it queries the app's `ValidatePaymentPromise` endpoint to verify that the signer's escrow account has sufficient available balance. This check reads the chain state at query time. Between validation and on-chain settlement (via `MsgPayForFibre` or `MsgPaymentPromiseTimeout`), the balance is not reserved. Two concurrent promises from the same signer can both pass validation against the same available balance, and the second one succeeds on-chain only if enough balance remains after the first settles.

This is a double-spend window at the validator level. A signer with 100 utia available can submit two 80 utia promises concurrently. Both pass validation. The first settles on-chain. The second either fails on-chain (wasting validator resources) or succeeds if it arrives before the first is processed. The validator has no local state to detect the conflict.

## Prerequisites

- [celestiaorg/celestia-app#6898](https://github.com/celestiaorg/celestia-app/pull/6898). This ADR assumes #6898 is merged. That PR allows payments to reduce pending withdrawals when `AvailableBalance` is insufficient. Withdrawals take 24 hours to execute. If a user initiates a withdrawal and then continues sending payment promises, the payments are deducted from the pending withdrawal amounts (oldest first). The user should avoid sending payment promises after initiating a withdrawal, but if they do, the withdrawal amount is reduced rather than the payment failing.

## Decision

Two options are presented. Option A adds a validator-local cache that tracks per-signer budget and pending promise reservations. The cache is used only by the `ValidatePaymentPromise` query path. Consensus execution in `msg_server.go` remains unchanged. Option B takes a different approach by reducing the double-spend window through parameter changes alone.

- **Option A** is protocol non-breaking. It uses periodic sweeps against chain state to reconcile the cache.
- **Option B** requires no code changes. It reduces `PaymentPromiseTimeout` to 5–10 minutes so the timeout agent settles promises faster, shrinking the double-spend window.

## Detailed Design

### Option A: Sweep-Based Cache

#### Cache Location and Storage

A new component `local_promise_cache.go` in `x/fibre/keeper/`. The cache lives entirely in memory. It is injected into the Fibre keeper at app wiring time as a non-consensus dependency. If nil (tests, non-validator nodes), the keeper falls back to current behavior.

Two record types are maintained:

**SignerBudget** — One entry per signer. Tracks the budget state for a single escrow account.

- Fields:
  - `last_known_balance` — The `AvailableBalance` read from chain state during the last sweep. The budget is computed relative to this value.
  - `available_budget` — Remaining budget for new promises: `last_known_balance - sum(pending promise amounts)`. Decremented on each reservation, reset on sweep.
  - `last_sweep_at` — Timestamp of the last sweep for this signer.
  - `ops_since_sweep` — Number of reservations since the last sweep. Used to determine staleness.

**PendingPromise** — One entry per accepted promise. Tracks a reservation that has not yet settled on-chain.

- Fields:
  - `signer` — The escrow account signer address.
  - `amount` — The reserved payment amount.
  - `created_at` — When the reservation was made.
  - `expires_at` — When the promise expires (used for eviction).

Internally, the cache maintains a map from signer to `SignerBudget`, a map from `promise_hash` to `PendingPromise`, and a signer-to-promises index for sweep enumeration.

#### Cache Eviction

If a signer has no new operations for longer than `PaymentPromiseTimeout + 1h` (i.e., all pending promises have either settled or expired and the timeout agent has had time to submit them), delete the signer's entire cache entry (SignerBudget and all PendingPromise records). This keeps the cache bounded to active signers only. The check is performed during reconciliation: if after dropping resolved/expired promises no pending promises remain and `now - last_reconcile_at` exceeds the eviction threshold, the signer is evicted. On the next validation for that signer, the cache is rebuilt from the chain state.

#### Restart Behavior

On restart, the cache starts empty. Signer budgets are populated on demand as new promises arrive — the first validation for a signer triggers a sweep against chain state to initialize its budget. Double-spend protection is temporarily lost for the period between restart and the first sweep for each signer. The minimum escrow bond bounds the damage during this window.

#### Concurrency

Validation is guarded per-signer with a mutex so two concurrent promises for the same signer cannot both consume the same remaining budget. Reservations are idempotent by `promise_hash`.

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
3. Drop any pending promise that is already processed on-chain (via `IsPaymentPromiseProcessed`).
4. Recompute: `remaining_budget = max(0, AvailableBalance - sum(kept promise amounts))`.
5. Reset `last_sweep_at = now`, `ops_since_sweep = 0`.
6. Update the in-memory state and remove dropped promise records.

Withdrawals do not need special handling. Withdrawals are not immediate — they have a 24-hour delay between request and execution. During this delay, the withdrawn amount is already subtracted from `AvailableBalance` on-chain. Since sweeps read the current `AvailableBalance`, any pending or processed withdrawal is always reflected before it takes effect. An hourly sweep cadence is well within the 24-hour withdrawal window.

`GasPerBlobByte` can change via governance. During a sweep, pending promise amounts are recomputed using the current params, so a parameter update is reflected within the next sweep cycle.

#### Minimum Escrow Balance

A minimum escrow balance should be introduced to safeguard against the selective-validator attack.

**The attack.** A client signs a different promise for each validator, sending each to a single validator. Each validator independently accepts the promise (passing its own local cache check), stores the blob, and signs it. The client intentionally never collects the 2/3+ signatures needed for the normal `MsgPayForFibre` path. When the promises expire, timeout agents submit all of them. Settlements succeed until the escrow is exhausted. Validators whose promises fail on-chain served data for free for at most ~2 hours (PaymentPromiseTimeout + timeout agent submission time) before dropping it.

**Why the minimum balance mitigates this.** A single PFF payment covers a week of data serving by the entire validator set. Even if the attacker sends a unique blob to every validator, one settled payment already covers all of them for a week. The attacker pays for a week of full-set serving and gets at most ~2 hours of free serving from validators whose promises don't settle.

The minimum escrow balance ensures this property holds. Set to `max_blob_size × gas_per_blob_byte × validator_set_size`, it guarantees that even after the attacker's regular promises have been processed, the escrow retains enough funds to settle the last round of promises sent to all validators as part of the attack. Without this minimum, the attacker could exhaust the escrow before the attack promises are submitted, leaving validators unpaid.

#### Rate-Limiting Sweeps

A malicious user could repeatedly submit promises from escrow accounts with zero or insufficient balance. Each submission fails the budget check, triggering a sweep-and-retry. Since the balance is still zero after the sweep, the promise is rejected — but the sweep already happened, reading chain state unnecessarily.

Repeated submissions for the same signer amplify this into a DoS on the state store. The cache should rate-limit sweeps for signers that fail with zero or insufficient balance — only re-sweeping at most once per block for such accounts. This bounds the state read overhead regardless of how many promises the attacker submits.

#### Tradeoffs

**Single-process cache.** The cache is local to a single process. In sentry setups with multiple validator nodes, each instance maintains its own cache. A client can submit different promises to different instances of the same validator, bypassing the per-instance budget. A standalone shared cache would solve this but is out of scope for this iteration.

**Cache poisoning via exposed gRPC endpoint.** The cache is updated through the `ValidatePaymentPromise` gRPC query. If the endpoint is exposed, a malicious user could submit crafted promises to drain any signer's cached budget to zero, forcing more frequent sweeps and state reads. Requiring stateless validation (signature verification) before updating the cache mitigates this — the attacker would need access to the signer's private key to produce a valid promise.

**Frontrunning.** A malicious user who intercepts a legitimately signed promise could submit it directly to the validator's gRPC endpoint before the real client's fibre upload reaches the server. However when the client subsequently submits the same promise to the fibre server, the server can still accept and start serving the data. The cache is idempotent by `promise_hash` — the same promise is not double-counted in the budget.

#### Related Improvements

- Sweeps read directly from app state, which can block gRPC requests during cache re-seeding. Implementing read-only state snapshots for gRPC queries ([celestiaorg/cosmos-sdk#728](https://github.com/celestiaorg/cosmos-sdk/issues/728)) would avoid contention between sweep reads and consensus writes.
- **Persisting the cache to disk.** The cache ideally could be backed by a prefixed namespace in the node's DB (outside IAVL) instead of living purely in memory. This would preserve reservations across restarts, eliminating the temporary loss of double-spend protection after restart. It would also allow atomic batch writes for consistency and survive process crashes without re-sweeping all active signers.

### Option B: Reduced Expiration Window (No Code Changes)

Instead of adding a cache, reduce the `PaymentPromiseTimeout` parameter from the current default (1 hour) to 5–10 minutes. The timeout agent submits expired promises shortly after expiration. With a shorter window, promises settle on-chain faster, and the period during which a double-spend can occur is reduced proportionally.

#### Why This Helps

The double-spend window exists between query-time validation and on-chain settlement. A shorter `PaymentPromiseTimeout` means:

- Promises expire sooner, so the timeout agent submits them sooner.
- The on-chain `IsPaymentPromiseProcessed` check catches duplicates sooner.
- A signer's `AvailableBalance` reflects settled promises sooner, so subsequent validations against chain state are more accurate.

#### Tradeoffs

**Does not eliminate double spending.** The double-spend window is reduced but not closed. Within the 5–10 minute window, concurrent promises to different validators can still pass validation. Coupled with rate limiting on the number of promises a signer can submit per time window, the double-spend surface can be further reduced.

## Consequences

### Option A

**Positive:**

- Closes the double-spend window at the validator level.
- No protocol changes — PaymentPromise format, client signing flow, and on-chain execution paths are unchanged.

**Negative:**

- Per-signer mutex serializes concurrent validations for the same signer. This is intended behavior to prevent oversubscription.
- Sweeps issue additional read requests against chain state (escrow balance, `IsPaymentPromiseProcessed` per pending promise) which increases query-path load on the state store.

### Option B

**Positive:**

- No code changes — governance parameter update only.

**Negative:**

- Does not eliminate double spending — only reduces the window.
- Tighter timing may push more promises to the timeout path.
