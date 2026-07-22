# ADR 029: Fibre Upload Rate Limiting

## Changelog

- 2026-07-21: Initial draft

## Status

Proposed

## Summary

The Fibre server writes every uploaded shard to disk. Nothing today caps how fast that disk fills, so a busy or hostile client can grow it without limit. This ADR adds a rate limiter to the upload path.

The limiter is one token bucket per validator. Each upload spends tokens equal to the blob size. When the bucket runs dry, the server rejects the upload and tells the client when to retry.

Shards are pruned after a fixed retention window (4h by default). So capping the fill rate caps the disk: `disk ≈ rate × window`. We do not pick the rate by hand. We pick a disk budget, and the rate follows from it.

A few transport-level caps (concurrent streams, connections, keepalive) bound memory alongside the disk bound.

## Context

Fibre is Celestia's off-chain, high-throughput data-availability path. A client erasure-codes a blob and sends each validator the rows assigned to it. Each validator's Fibre server stores its assigned rows (its shard) on local disk.

The only operation that grows this store is the `UploadShard` RPC (`fibre/server_upload.go`), which ends in a `store.Put`. Pruning and reconciliation only delete. Nothing limits how fast uploads arrive, so nothing limits how fast the disk fills. A validator has no defense against a client, friendly or not, that uploads faster than planned.

There are limits today, but none of them bound throughput. A per-message size cap bounds a single request. A pool of verification workers (`ServerConfig.UploadVerifyWorkers`, default `GOMAXPROCS`) bounds CPU. The `grpc.NewServer` call (`fibre/internal/grpc/server.go`) sets no `MaxConcurrentStreams`, so a peer can open many streams at once and pin a large amount of receive memory. None of this caps total bytes to disk.

What saves us is that shards expire. Each one is pruned after a retention window. By default that window is 4h, and governance can change it. (The exact deadline is `pruneAt = max(creation + ShardRetention, ExpiresAt)`, with `ShardRetention` of 4h and `ExpiresAt` one hour after creation; the 4h term wins, so effective retention is 4h. A once-a-minute loop in `fibre/server_prune.go` enforces it.) Uploads flow in at some rate and flow out one window later, so the store settles at about `rate × window` bytes. That is the lever. Bound the rate and you bound the disk.

We want this for two reasons. In the near term it lets us run Fibre on mainnet during the 2026 ramp-up without a surprise in disk usage. In the long term validators run on fixed hardware and do not autoscale, so a disk bound is always useful. This is a permanent mechanism, not a throwaway guard.

One question stays open: what should the limiter meter, and at what scope? There are five candidates, referred to by number below:

1. **Global throughput** — one flat charge per upload, the same for every validator.
2. **Shard size × stake** — charge each validator for the bytes it actually stores.
3. **PFF (PayForFibre) size × stake** — a flat charge, but a per-validator budget scaled by stake.
4. **Per client / escrow / signer** — a second limit keyed on who is paying.
5. **Occupancy gating** — admit based on how full the store is right now, not on a token rate.

Choosing the actual number is a separate question, handled in Sizing.

### Relevant mechanics

Three facts about Fibre shape the design.

- **Assignment is stake-weighted.** A validator with stake fraction `s` is assigned about `3s` of the rows (clamped to a minimum and to the full set; see `Set.Assign` in `fibre/validator/set.go`). Bigger validators store more per blob. Disk already scales with voting power, before any limiter.
- **The charge is the blob size.** `UploadSize` (`fibre/blob.go`) is the padded blob size, the same for every validator. It counts only the blob's rows. A validator stores a little more than its share of that: it also keeps per-row proofs, the RLC vector, and row indices. So its disk per blob is roughly `min(1, 3s) × MaxShardSize`, at most one full shard.
- **A client needs a quorum.** A client uploads to every validator and needs signatures from more than `2/3` of stake to settle on-chain. Its client library (`ClientCache.Request` in `fibre/internal/grpc/client_cache.go`) does not retry application errors. So a rejection must not silently cost the client its quorum.

### Requirements

- **R1** — Bound each node's disk to a budget we can derive, not guess, over the retention window.
- **R2** — New clients or heavier usage must not break correctness or force a redesign. Fairness between clients is nice to have, but separate.
- **R3** — Keep the `>2/3` quorum reachable while the limiter is throttling.
- **R4** — Keep it simple: config-gated, tunable, removable, no protocol change.

### Out of scope

Two nearby things are excluded. The first is limiting throughput inside block production (`PrepareProposal` / `ProcessProposal`). That path writes to goleveldb, so any limit there is a goleveldb problem, and reworking it is not near-term. It may come back if goleveldb is revisited. The second is the read path (`DownloadShard`), which never grows the store.

## Decision

### Sizing — pick a disk budget, derive the rate

We do not choose the rate directly. We choose how much disk a validator should spend on Fibre, and the rate follows. That way we tune disk itself, which is what we care about.

Start with the bucket. Over any period `T` it admits at most `burst + rate × T` bytes. A shard lives for one retention window before it is pruned, so at steady state a top-stake validator holds at most:

> `peak = burst + rate × window`

Smaller validators hold a stake-weighted fraction of that. We want the peak to equal the disk budget, which fixes the rate:

> `rate = (budget − burst) / window`

That leaves `burst`: how much the bucket lets through at once, before the steady rate takes over. Two constraints set it.

- **Floor:** at least one max blob (128 MiB). A single draw larger than `burst` is refused by `golang.org/x/time/rate`, so a smaller burst would reject every full-size blob outright.
- **Target:** half a window of rate. This lets a client that has been quiet catch up in a burst, instead of being paced from its first byte.

> `burst = max(128 MiB, rate × window / 2)`

When the target dominates, solving the two equations together gives `rate = 2 × budget / (3 × window)` and `burst = budget / 3`.

One caveat for provisioning. The bucket counts blob bytes (`UploadSize`), but a stored shard is about 1.4% larger, because it also holds proofs, the RLC vector, and indices. So provision about `budget × 1.014`, plus headroom.

Operators pick only the disk budget for their stake share. Rate and burst follow. The rate is the same for every validator; Consensus impact explains why it has to be.

### Model — global now, per-signer maybe later

We use **Model 1**: one token bucket per validator, charging the flat `UploadSize` on each upload.

Global works because the assignment already does the per-stake weighting for us. Disk is bounded and stake-proportional without any extra math, and every validator charges the same blob the same amount. So a blob is either admitted everywhere or rejected everywhere, never half-admitted across the set. Charging per-validator amounts (Models 2 and 3) would break that and risk a split quorum. Occupancy gating (Model 5) is more precise but heavier to build.

Model 1 gives no fairness between clients. That is fine now: there is one client, and the global cap bounds disk no matter how many clients appear. **Model 4**, a per-signer sub-limit under the same global cap, is left for later, when there is real contention to manage. It can be added without breaking anything, because clients see the same rejection either way, so there is no reason to build its per-signer state and Sybil policy before they are needed. If contested multi-client use is expected early, Model 4 can ship in v1 instead.

### Rejection — reject-fast

When the bucket is empty, the server rejects the upload right away. It returns `ResourceExhausted` with a retry-after hint, and the client waits or tries another validator. It does not hold the request open waiting for tokens.

Blocking would be worse: it ties up server handlers, spreads unpredictable latency across validators, and lets one client stall others. Reject-fast avoids all of that. It does need a client change (the client must honor the hint and keep its quorum), and we make that change now, while Fibre has no production users. It is harmless today and would be breaking once clients rely on the server absorbing the wait.

Rejecting has one cost: an over-budget reject sends the client off to reroute, which can pile up if the next node is also busy (Wondertan raised this on the PR). A short queue that blocks briefly instead of rejecting would smooth that out near the boundary. We leave it as a possible later refinement, for two reasons. First, the burst already smooths spikes: with a half-window burst the bucket rarely empties, so the queue would seldom fire, and under real overload it degrades to rejecting anyway. Second, since the charge happens after verification, a blocked request is a live handler holding its full message (~132 MiB) and a stream slot, so a queue would need a dedicated size bound, the very in-flight cap this design otherwise drops. See Alternative Approaches.

## Detailed Design

### What happens on an upload

For each `UploadShard` request the server:

1. Verifies the payment promise, the assignment, and the shard rows (`verifyPromise`, `verifyAssignment`, `verifyShard` in `fibre/server_upload.go`). A failure here returns an error, unchanged from today.
2. Checks whether the shard is already stored. If it is (a replay), the server returns OK and stops. It does not spend tokens.
3. Otherwise, takes `UploadSize` tokens from the bucket. If there are not enough, it rejects with `ResourceExhausted` and a retry-after hint.
4. Stores the shard and signs.

The token charge sits between step 2 and step 4, so a replay never drains the bucket, and only shards that actually reach disk are charged.

### The limiter

- **Bucket.** A `golang.org/x/time/rate` limiter. Tokens are bytes. Each upload charges `UploadSize`. Sized from the disk budget as in Sizing.
- **Charge against server time.** The limiter uses the server's clock, never the client's `CreationTimestamp`, which a client controls.
- **Replay check.** The charge is gated on a store-presence check, so a replay of an accepted shard cannot drain the bucket. There is no such check today (`fibre/store.go` has `Put` and `Get`, no `Has`), so a cheap presence lookup has to be added. This relates to ADR-025's promise cache but does not depend on it: if that cache lands, the check can read from it; if not, a direct store lookup is enough.
- **Rejection.** `ResourceExhausted` plus a retry-after delay in a gRPC `RetryInfo` detail.
- **Metrics.** Add two counters under the existing `fibre.server.upload_shard.` namespace (`fibre/server_metrics.go`): `rate_limited` (labeled by reason) and `admitted_bytes`. The existing `fibre.server.upload_shard.bytes` counts received bytes, which is not the same as the charged `UploadSize`.
- **Off switch.** A no-op when disabled or when the derived rate is not positive.

### Transport-level limits

The rate limiter bounds disk, not memory. gRPC reads a whole `UploadShard` message into memory (up to `MaxMessageSize`, about 132 MiB: the 129.83 MiB shard plus the promise and ~2% protobuf framing, larger than the 128 MiB blob) before the handler runs. A rejected upload has already been received. Receive memory is capped only at the transport layer, by `MaxRecvMsgSize × streams × connections`, and none of those factors is bounded today.

This work adds three transport caps, as gRPC server options and a wrapped listener:

- **`MaxConcurrentStreams`** — limit in-flight RPCs per connection.
- **Max connections** — a limiting listener caps total connections, so a peer cannot dodge the per-connection stream cap by opening many connections.
- **Keepalive** (`KeepaliveEnforcementPolicy`, `KeepaliveParams`) — drop idle or abusive connections and blunt slow-read holding.

Together these bound receive memory and connection load. With them plus the verifier pool, the old application-level in-flight cap is no longer needed and is dropped. These are fixed, protocol-sane values, not derived from the disk budget.

### Configuration and tuning

The disk budget and the enable flag are the config inputs (`ServerConfig`, TOML and CLI). Rate and burst are derived from the budget (see Sizing), not set on their own. The limit can be raised as usage grows or turned off entirely, with no protocol change.

The rate is meant to be a governance parameter, not free per-operator config, so that every validator uses the same value (see Consensus impact). Locally, an operator can still turn the limiter off, which only risks that node's own disk. What an operator must not do is set a lower rate than peers: that throttles blobs others accept and can deny the `>2/3` quorum.

There is one invariant to enforce at startup: `burst ≥ MaxBlobSize`. The derivation already guarantees it (it is the floor in `burst = max(MaxBlobSize, rate × window / 2)`), but a manual override could break it, and `golang.org/x/time/rate` fails silently when a single draw exceeds the burst. So `ServerConfig` validation should reject a config where `burst < MaxBlobSize`, alongside the existing checks.

### Per-signer sub-limit (Model 4)

Model 4 adds a second bucket, keyed on `PaymentPromise.SignerKey` and charged after the global check passes, with per-signer state evicted like ADR-025's cache. A Sybil client (many signer accounts) is handled on two levels. The global bucket still bounds disk whatever the signer count, so Sybil can only affect fairness. And sizing each signer's share by its escrow balance ties a client's total share to its total escrow: N accounts need N times the escrow for N times the share. (An equal split would instead need a cap on how many signers can be active.)

### Sizing example

Using the defaults (`fibre/protocol_params.go`, `x/fibre/types/params.go`): `OriginalRows = 4096`, `MaxRowSize = 32 KiB`, `window = 4h`.

- Max charged blob: `UploadSize = 4096 × 32 KiB = 128 MiB` (this is `MaxBlobSize`).
- Max stored shard: `MaxShardSize = 129.83 MiB`, the 128 MiB of rows plus 1.83 MiB of overhead (proofs `4096 × 448 B`, RLC `4096 × 16 B`, indices `4096 × 4 B`). That is the ~1.4% gap between charged and stored bytes.

A worked rate. Take a disk budget of 1 TiB. The target burst dominates the floor, so `rate = 2 × 1 TiB / (3 × 4h) ≈ 48.5 MiB/s` and `burst = 1 TiB / 3 ≈ 341 GiB`. Peak charged bytes stay at 1 TiB, and the operator provisions about `1.014 TiB` of disk plus headroom. No budget is fixed yet, so the number is still open; whatever it is, `rate = (budget − burst) / window` with `burst = max(128 MiB, rate × window / 2)`.

### Consensus impact

All five models run off-chain. The limiter lives in the Fibre server, outside the ABCI state machine, and only decides which uploads a validator accepts and signs. It does not touch block validity or determinism: validators with different settings still agree on every block and differ only in what they choose to sign. Two things do interact with consensus.

- **Quorum.** `MsgPayForFibre` is settled on-chain against signatures from more than `2/3` of validators (`x/fibre/keeper/msg_server.go`). The limiter does not change that rule, but throttling can stop a client from reaching `2/3` and fail its settlement. That is why the rate must be the same for every validator: if some run a lower rate, they reject blobs others accept, and the client loses quorum through no fault of its own.
- **The rate as a governance parameter.** Because uniformity is a quorum requirement, the rate belongs in governance. An `x/fibre` module parameter makes every validator read the same value by construction, instead of trusting operators to coordinate local config. Promoting it is a normal versioned-upgrade change, not a fork, and enforcement still lives in the off-chain server. Routine tuning is not urgent, and the emergency path (turning the limiter off locally) stays local and is quorum-safe. The recommendation is to make the rate a gov parameter from v1; coordinated local config is acceptable only as a ramp-up bridge, with a commitment to promote it.

The kind of limiter that *is* consensus-critical, one inside `PrepareProposal` / `ProcessProposal` that must be deterministic across validators, is out of scope.

## Alternative Approaches

The chosen design is Model 1 as the base, optional Model 4 for fairness, and reject-fast rejection (with a bounded queue left as a possible refinement). The options:

**Model 1 — global throughput (chosen).** One flat `UploadSize` bucket per validator. Bounds per-node disk automatically and in proportion to stake, keeps admission uniform, and is the simplest to build and remove. Weaknesses: no client isolation on its own, and a low-stake validator is metered by the full blob size even though it stores less.

**Model 2 — shard size × stake.** Charge the bytes each validator actually stores. This is the most direct disk accounting, but validators then charge different amounts for the same blob, so a blob can be admitted by some and rejected by others. That risks a split quorum and a non-deterministic signer set.

**Model 3 — PFF size × stake.** A flat charge with a per-validator budget scaled by stake. With a single network rate this is just Model 1 (assignment already makes disk stake-proportional). With truly per-validator budgets it brings back Model 2's split-quorum risk. No net gain.

**Model 4 — per client / escrow / signer (chosen, deferred).** A per-signer, escrow-weighted bucket. Gives real client isolation, but cannot bound total disk on its own (it still needs the global cap) and adds per-signer state. Deferred until there is contention to justify it.

**Model 5 — occupancy gating.** Admit based on current store size versus budget. The most direct disk enforcement, since a token bucket refills even when nothing is stored, but it needs live occupancy tracking and back-pressure and is burstier near the cap. A possible later refinement.

**Rejection — reject-fast (chosen) vs block-and-wait vs hybrid.** Block-and-wait needs no client change today but adds head-of-line blocking, unpredictable latency, and a contract that is breaking to change later. Reject-fast avoids that, at the cost of turning an over-budget upload into a reroute that can cascade across busy nodes. A hybrid, a short queue that blocks while the wait stays under the reroute cost and then rejects, would recover the near-boundary latency. It is deferred: the half-window burst already smooths spikes, so the queue would rarely fire, and a blocked request holds its full ~132 MiB message and a stream slot, so the queue would re-introduce the in-flight cap this design drops. Revisit it if a small burst is chosen or the small-request case shows up in practice.

**Temporary hardcoded guard.** Rejected. The limiter is permanent, so a throwaway fixed rate (say a placeholder 10 MiB/s) is wasted work and starves easily. A conservative Model 1 setting covers the ramp-up just as well.

## Consequences

### Positive

- One permanent mechanism covers both the ramp-up and steady state.
- Disk is bounded, and the bound comes from a budget rather than a guess.
- A uniform charge keeps admission predictable and quorum-safe.
- Per-signer fairness can be added later without a redesign.
- The replay-drain hole is closed.
- Transport caps bound receive memory, which was unbounded, and remove the need for the old in-flight cap.

### Neutral

- Sizing does not depend on the model.
- Per-node disk scales with stake, which is intended: more stake, more commission.
- `upload_shard.admitted_bytes` (charged) and `upload_shard.bytes` (received) measure different things.
- No proto or consensus change; the limit is raised or disabled by config.

### Negative

- Reject-fast needs a client change before clients depend on the limiter.
- Model 1 alone has no client fairness; that waits on the per-signer layer.
- A low-stake validator is metered by the full blob size.
- An over-budget reject costs the client a reroute; the queue that would soften it is deferred.
- The download path stays unthrottled (out of scope).

## References

- PROTOCO-1547 — tracking issue.
- PR #7481 — draft upload admission controller (not merged); the design discussion behind this ADR.
- PR #7489 — pruning window reduced to 4h.
- `x/fibre/types/params.go` — `ShardRetention` (4h), `PaymentPromiseTimeout` (1h).
- ADR-025 — Fibre local promise cache (per-signer accounting, replay mitigation).
- ADR-027 — single-sequencer BFT ordering on Fibre.
- `fibre/validator/set.go` — stake-weighted row assignment.
