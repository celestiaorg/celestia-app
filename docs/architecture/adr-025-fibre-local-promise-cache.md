# ADR 025: Fibre Local Promise Cache

## Changelog

- 2026-03-27: Initial draft
- 2026-03-29: Add nonce-based option (Option B)
- 2026-03-31: Remove Option B, rename Option C to Option B
- 2026-08-10: Add Option C and Option D

## Status

Proposed

## Context

When a validator receives a blob upload via fibre, it queries the app's `ValidatePaymentPromise` endpoint to verify that the signer's escrow account has sufficient available balance. This check reads the chain state at query time. Between validation and on-chain settlement (via `MsgPayForFibre` or `MsgPaymentPromiseTimeout`), the balance is not reserved. Two concurrent promises from the same signer can both pass validation against the same available balance, and the second one succeeds on-chain only if enough balance remains after the first settles.

This is a double-spend window at the validator level. A signer with 100 utia available can submit two 80 utia promises concurrently. Both pass validation. The first settles on-chain. The second either fails on-chain (wasting validator resources) or succeeds if it arrives before the first is processed. The validator has no local state to detect the conflict.

## Prerequisites

- [celestiaorg/celestia-app#6898](https://github.com/celestiaorg/celestia-app/pull/6898). This ADR assumes #6898 is merged. That PR allows payments to reduce pending withdrawals when `AvailableBalance` is insufficient. Withdrawals take 24 hours to execute. If a user initiates a withdrawal and then continues sending payment promises, the payments are deducted from the pending withdrawal amounts (oldest first). The user should avoid sending payment promises after initiating a withdrawal, but if they do, the withdrawal amount is reduced rather than the payment failing.

## Decision

Four options are presented. Options A and B are the two building blocks. Option C runs both together with a rate limit. Option D moves the reservation into consensus state to close the multi-node gap that the local options leave open, and pays for it with a protocol change.

- **Option A** is protocol non-breaking. It adds a validator-local cache reconciled by periodic sweeps against chain state. Only the `ValidatePaymentPromise` query path uses the cache; consensus execution in `msg_server.go` is unchanged.
- **Option B** requires no code changes. It reduces `PaymentPromiseTimeout` toward its 10-minute floor (`MinPaymentPromiseTimeout`) so the timeout agent settles promises faster, which shrinks the double-spend window.
- **Option C** runs A, B, and a per-signer rate limiter as one query-path mechanism. It stays in memory and off the consensus path, so it adds no latency to fibre uploads.
- **Option D** moves the reservation into consensus state, either a per-promise on-chain reservation or a bond-backed payment channel. It is the only design that closes the aggregate multi-node double-spend, and it is a breaking protocol change.

See the Conclusion for the recommendation. Decision: TBD

## Detailed Design

### Option A: Sweep-Based Cache

#### Cache Location and Storage

A new component `local_promise_cache.go` in `x/fibre/keeper/`. The cache lives entirely in memory. It is injected into the Fibre keeper at app wiring time as a non-consensus dependency. If nil, the keeper falls back to current behavior.

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

Internally, the cache maintains a map from signer to `SignerBudget`, a map from `promise_hash` to `PendingPromise`, and a signer-to-promises index for sweep enumeration.

#### Cache Eviction

If a signer has no new operations for longer than `PaymentPromiseTimeout + 1h` (i.e., all pending promises have either settled or expired and the timeout agent has had time to submit them), delete the signer's entire cache entry (SignerBudget and all PendingPromise records). This keeps the cache bounded to active signers only.

A background goroutine runs periodically and scans all signer entries, evicting any that exceed the threshold. This ensures idle signers are cleaned up even if they never receive another promise. On the next validation for an evicted signer, the cache is rebuilt from chain state.

#### Restart Behavior

On restart, the cache starts empty. Signer budgets are populated on demand as new promises arrive — the first validation for a signer triggers a sweep against chain state to initialize its budget. Double-spend protection is temporarily lost for the period between restart and the first sweep for each signer.

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

### Option C: Hybrid (Cache + Reduced Timeout + Rate Limiting)

Options A and B are layers of the same defense, not competing choices. Option C runs both at once and adds a per-signer rate limit.

The three layers cover different parts of the problem:

- **A (local cache)** closes the double-spend window inside a single process, exactly, in steady state. Two caveats bound the word "exact." After a restart the cache starts empty and rebuilds lazily on the first sweep per signer, so the in-process guarantee has a gap until then (see Option A, Restart Behavior). And the cache must reserve against the same balance the on-chain path spends: today the query path checks total `Balance` (which includes funds locked in pending withdrawals), while the sweep in Option A budgets against `AvailableBalance`. Which balance is authoritative for the reservation must be settled before implementation, or the cache and consensus disagree on what "sufficient" means.
- **B (reduced timeout)** shrinks the window in which unsettled promises pile up. Settlements lower the committed balance sooner, so later validations start failing sooner.
- **Rate limiting** caps how much one signer can get validated per time window, per process. It lowers each instance's worst-case loss and protects the state store from a sweep or query flood.

#### Rate Limiting

Two limits live in the same per-signer `SignerBudget` record from Option A. They share its map and its per-signer mutex, so no new locking is added.

1. **Validation rate limit.** A per-signer token bucket over promise value per time window (value, not count, so the bound lines up with the `min(B, R*T)` analysis below). It bounds how fast a signer can pile up unsettled promises against one instance.
2. **Sweep rate limit.** The limit already described under Option A ("Rate-Limiting Sweeps"). A signer that fails with zero or insufficient balance is re-swept at most once per block, which protects the state store from sweep amplification.

A rejected query returns gRPC `ResourceExhausted` with `RetryInfo`, the same contract the admission controller in ADR-029 uses. This reuse is a dependency on ADR-029 rather than a standalone guarantee: it assumes the ADR-029 admission controller has shipped and that clients already honor `RetryInfo` with a bounded backoff. Where that holds, the client needs no new code.

#### Residual Exposure

Option C bounds the aggregate multi-node double-spend but does not close it. The cache and the rate limiter both live in per-process memory, so `M` counts independent processes, not validators: it is the validator count multiplied by the sentry/replica fan-out that can each serve the query path. There is no shared state between these `M` authorizers. Each reads the same committed balance, and that balance is stale until a settlement lands on-chain, so each authorizes on its own.

With reduced timeout `T` and validation rate `R`, the worst case is:

```text
per-instance loss:  min(B, R*T)
aggregate:          M * min(B, R*T)
```

For example, a signer with balance `B = 100` facing `R*T = 30` loses at most 30 per instance instead of the full 100. The rate limit sets the `R*T` term, and the reduced timeout sets `T`. Here `R` is a rate in value per unit time: normalize `PromiseValidationRate` over `PromiseValidationWindow` before multiplying by `T`, so `R*T` is a value and not a value×window product.

Three existing bounds clamp the residual aggregate, though none of them refund the loss:

- The ADR-029 occupancy limiter caps the total fibre disk a validator provides, paid or not. This bounds how much unpaid data can *accumulate* on disk; it does not bound the bandwidth and serving cost already spent to deliver an unpaid upload. Disk is capped, service is not.
- `ShardRetention` bounds how long unpaid shards stay before pruning.
- A signer whose promises keep failing to settle is visible on-chain. A later reputation or ban layer could act on that pattern.

Tune `R` and `T` so that `M * min(B, R*T)`, for a realistic `M` (validator count plus sentry fan-out), stays below the cost of running the attack.

#### New Parameters

- `PromiseValidationRate`. Token-bucket rate (promise value per window) for the validation rate limit.
- `PromiseValidationWindow`. The window the rate is measured over.
- `SweepRateLimit`. Maximum sweeps per block for failing signers, default once per block.
- `PaymentPromiseTimeout`. Reduced default from Option B. Option B quotes a 5–10 minute range, but the enforced floor is `MinPaymentPromiseTimeout` = 10 minutes, so 10 minutes is the effective setting Option C adopts. Going lower requires lowering that floor, which interacts with `MinWithdrawalDelay = MaxPaymentPromiseTimeout + MinTimeoutSettlementWindow` and removes the upload-path headroom the floor protects.

Because Option A evicts idle signers at `PaymentPromiseTimeout + 1h`, the reduced timeout also shortens the eviction horizon (to roughly 1h10m). This only changes when idle cache entries are reclaimed, not correctness.

### Option D: On-Chain Reservation

Options A through C keep the reservation in per-process memory, which is why the aggregate multi-node case stays open. Option D puts the reservation in consensus state, which every one of the `M` authorizers reads. The first reservation locks the funds. Any later promise, on any node, sees `reserved + amount > balance` and is rejected. The `M` separate authorizers now share one state, so the aggregate multi-node double-spend is closed.

One invariant must hold on every path that touches an escrow (deposit, reserve, settle, timeout, and both withdrawal request and execution):

```text
reserved + pending_withdrawals <= balance
```

The chain cannot see whether an off-chain upload was actually served, so a reservation has to commit funds before service is confirmed. That does not remove the risk, it moves it. The expiry policy chooses who carries it:

- **Deduct on expiry** (today's timeout behavior). A reservation is a firm spend. Validators are always paid, and the signer pays even for a blob that was never served.
- **Release on expiry.** A reservation is a refundable hold. The signer never pays for non-service, and a validator risks not being paid if settlement misses the expiry under congestion or censorship.

Two variants trade completeness against fibre latency.

**D-strict: per-promise on-chain reservation.** A new `Reservation` record and a `MsgReservePromise` create the reservation, and `MsgPayForFibre` or `MsgPaymentPromiseTimeout` settle it. One upload runs as follows: the client submits `MsgReservePromise`, waits for it to commit, then uploads, and the validator serves only after it reads the committed reservation. This closes the double-spend, but every upload now costs an on-chain write and one block of latency before serving, which is the property fibre exists to avoid. Locking funds does make reservations self-limiting, since state growth is bounded by balance.

**D-amortized: bond-backed payment channel.** The signer posts one on-chain bond up front. Promises stay off-chain and carry a monotonic `(seq, cumulative_amount, commitment)`. A validator serves when `cumulative_amount <= bond`, reading the bond from cached, slowly-changing state (the ADR-029 recompute pattern), so the hot path keeps no consensus round-trip and fibre speed is preserved. Settlement is a periodic on-chain redemption that ratchets a per-signer `redeemed_cumulative` forward by the delta. Taking the maximum cumulative rather than summing deltas avoids head-of-line blocking across validators. Double-spend stays possible but becomes provable: two promises signed at the same `cumulative` for different commitments are a slashable double-sign, and the loss is capped by the bond. A single per-signer `redeemed_cumulative` shared across independent validators needs care of its own: with one global counter, a redemption by one validator advances the watermark past promises another validator still holds at a lower cumulative, stranding them. Closing this requires per-`(signer, validator)` cumulative accounting (sub-channels), not one global counter. The max-cumulative rule only prevents double-counting the signer's own spend, not allocation across validators. The economic guarantee only holds while the forfeited bond exceeds the attacker's gain from the double-spend; a bond sized below the value a signer can extract in one window is not a deterrent, so the bond floor has to track worst-case per-window exposure. The costs are a fraud-proof message, slashing logic, locked bond capital, and a short stall whenever the bond is funded or topped up.

#### Tradeoffs

**Protocol change.** Both variants change the on-chain payment path with new state, new messages, and (for D-amortized) slashing. This is a breaking change, which Options A through C are not.

**Speed against completeness.** D-strict is the only design that makes double-spend impossible, and it charges every upload a consensus round-trip. D-amortized keeps fibre speed and closes the aggregate case economically, capped by the bond (only as long as the bond is sized above per-window exposure), at the cost of fraud-proof and slashing machinery.

**Gas-price slippage.** If `1 gas = 1 utia` is ever replaced by a variable gas price, the reserved amount and the settled amount can drift apart. The reservation then has to lock at a price the settlement will honor.

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

### Option C

**Positive:**

- Runs the exact per-process protection of A together with the rate and DoS bounds of B and rate limiting, as one mechanism.
- No protocol change and no added latency on fibre uploads. Everything stays in memory on the query path.
- Reuses the ADR-029 client contract (`ResourceExhausted` with `RetryInfo`).

**Negative:**

- Does not close the aggregate multi-node double-spend. It bounds per-instance loss to `min(B, R*T)`.
- Adds governance parameters that have to be tuned against real validator and sentry topology.

### Option D

**Positive:**

- The only option that closes the aggregate multi-node double-spend, by putting the reservation in shared consensus state.

**Negative:**

- Breaking protocol change (new state, new messages, and slashing for D-amortized).
- D-strict adds a consensus round-trip and gas to every fibre upload, which degrades the low latency fibre exists to provide.
- D-amortized needs fraud-proof and slashing infrastructure and locks bond capital.

## Conclusion

The four options range from cheap with a bounded residual to complete with a real cost.

- **Options A and B** are building blocks, not full answers on their own. A alone leaves the multi-node gap. B alone only shrinks the window.
- **Option C** is recommended for v1. It runs A, B, and rate limiting as one query-path mechanism. It breaks no protocol, adds no upload latency, closes the in-process double-spend exactly in steady state (with the post-restart gap and the `Balance`-vs-`AvailableBalance` reconciliation called out above), and holds each instance's residual loss to `min(B, R*T)` with no cross-node coordination. The aggregate multi-node case it leaves open is bounded (though not refunded) by the ADR-029 occupancy limiter (disk accumulation, not serving cost), `ShardRetention` (duration), and on-chain detectability, which makes it a bounded cost rather than an open hole.
- **Option D** is the only design that closes the aggregate multi-node double-spend, because a reservation in consensus state is shared by every authorizer. D-strict pays for that with a consensus round-trip on every upload, which defeats fibre's purpose. D-amortized keeps the speed but needs a bond-backed payment channel with fraud proofs and slashing.