# ADR 029: Fibre Upload Rate Limiting

## Changelog

- 2026-07-21: Initial draft

## Status

Proposed

## Context

Fibre is the off-chain, high-throughput data-availability path. A client erasure-codes a blob and fans its rows out to the validators. Each validator's Fibre server receives the shard assigned to it and writes that shard to local disk. The only ingress that grows this store is the `Server.UploadShard` RPC (`fibre/server_upload.go`), which ends in a `store.Put`. Everything else that touches the store, pruning and reconciliation, only deletes.

Nothing today bounds how fast a server accepts uploads, so nothing bounds how fast its disk grows. The two limits that exist do not help here: a per-message size cap (`MaxRecvMsgSize` / `MaxSendMsgSize`, set in `fibre/server.go`), and a pool of verification workers sized by `ServerConfig.UploadVerifyWorkers`, which defaults to `GOMAXPROCS` and is wired up in `fibre/server.go`. The `grpc.NewServer` call in `fibre/internal/grpc/server.go` also passes no `MaxConcurrentStreams`, so the number of in-flight streams, and therefore the receive-buffer memory a peer can force the server to hold, is unbounded.

Stored shards do not live forever. Each one is pruned once its deadline passes, `pruneAt = max(creation + ShardRetention, ExpiresAt)`, where `ShardRetention` is 4h and `ExpiresAt = creation + PaymentPromiseTimeout` with `PaymentPromiseTimeout` of 1h (see `x/fibre/types/params.go` and `x/fibre/keeper/keeper.go`). Effective retention is 4h, and both parameters are governance-tunable. A loop in `fibre/server_prune.go` runs once a minute and enforces the deadline. Shards flow in at some admitted rate and flow out after a fixed window, so the store settles at **disk ≈ admitted rate × retention window**. Bound the admitted rate and you bound the disk.

We want this bound for the 2026 ramp-up, so Fibre can run on mainnet without a surprise in disk usage, and we want it permanently, because validators run on fixed hardware and do not autoscale. The limiter is permanent, not a throwaway guard. A conservative early setting is one operating point on a knob we keep.

The design question is what the limiter meters and at what scope. Five models are on the table:

1. **Global throughput** — one flat charge per upload, identical for every validator, with no per-client or per-stake weighting.
2. **Shard size × stake** — charge each validator for the bytes it actually stores, which vary with its stake-weighted assignment.
3. **PFF (PayForFibre) size × stake** — charge the uniform `UploadSize`, but give each validator a per-stake budget.
4. **Per client / escrow / signer** — a second limit keyed on who pays, so one heavy client cannot crowd out the rest.
5. **Occupancy gating** — admit based on how full the store currently is, rather than on a token rate.

Choosing the numeric rate (sizing) is a separate axis from the model, covered below.

### Relevant mechanics

- **Assignment is stake-weighted.** For stake fraction `s`, `Set.Assign` (`fibre/validator/set.go`) hands out `clamp(ceil(3s · originalRows), MinRowsPerValidator, originalRows)` rows against a liveness threshold of `1/3`. A validator's shard, and its disk per blob, scales with its voting power.
- **Charge basis.** `UploadSize = originalRows × rowSize` (`fibre/blob.go`) is the padded original blob size and is the same for every validator. It counts only the original row data. A validator writes more than that: its assigned fraction of the rows plus per-row proofs, the RLC vector, and row indices. Its stored bytes are roughly `min(1, 3s) × MaxShardSize`, at most one full shard for `s ≥ 1/3`, never the 4× of the erasure-coded parity. The 4× exists only as a network aggregate, never on one node.
- **Client behavior.** A client uploads to every validator and needs signatures from more than `2/3` of stake to settle. Client-side `ClientCache.Request` (`fibre/internal/grpc/client_cache.go`) does not retry application errors, so any rejection the limiter returns must not silently cost the client its quorum.

### Requirements

- **R1** — Bound each node's disk to a budget that can be derived, not guessed, over the retention window.
- **R2** — New clients or higher usage must not break correctness or force a redesign. Fairness between clients is desirable but separate.
- **R3** — Keep the `>2/3` settlement quorum reachable while throttling is active.
- **R4** — Simple, config-gated, tunable, removable, no protocol change.

### Out of scope

Two nearby things are excluded. Throughput limiting inside `PrepareProposal` / `ProcessProposal` writes to goleveldb (the block-store database), so it is a goleveldb-specific concern, and reworking that path is not near-term; a consensus-layer limit may return if goleveldb is revisited. The read path (`DownloadShard`) does not grow the store, so it is irrelevant to the disk bound.

## Decision

### Sizing — derive the rate from a disk budget

The rate is one protocol-wide value derived from a disk budget, not a hand-picked number. This inverts the naive framing: the invariant is storage held over the pruning window, so start from the budget and derive the rate, rather than picking a rate and hoping the disk lands somewhere reasonable.

A token bucket admits at most `burst + rate × T` over any interval `T`, and each admitted shard lives for the retention window, so a node holds at most this many charged bytes:

> `peak_charged = min(1, 3s) × (burst + rate × retention_window)`

For the reference (maximum-stake) validator `min(1, 3s) = 1`, and we want `peak_charged` to equal `reference_disk_budget`. That fixes the relation between rate and burst:

> `reference_disk_budget = burst + rate × retention_window`

`burst` is derived, not chosen. It is the larger of a floor and a target. The floor is `MaxBlobSize`, so any single blob fits in one draw (`golang.org/x/time/rate` fails a draw larger than `burst`). The target is half a window of rate, `rate × retention_window / 2`, which lets an idle client catch up with a bounded backlog instead of being paced from the first byte:

> `burst = max(MaxBlobSize, rate × retention_window / 2)`

When the window term dominates, substituting it back gives `reference_disk_budget = 1.5 × rate × retention_window`, so `rate = 2 × reference_disk_budget / (3 × retention_window)` and `burst = reference_disk_budget / 3`. When the budget is small enough that `MaxBlobSize` dominates, `burst = MaxBlobSize` and `rate = (reference_disk_budget − MaxBlobSize) / retention_window`. Either way `peak_charged` stays at the budget.

Actual disk runs slightly above charged bytes, since each shard also stores proofs, the RLC vector, indices, and a little metadata (the Sizing example works the numbers; the ratio is about 1.014). Operators should provision `peak_disk ≈ peak_charged × MaxShardSize / UploadSize` plus headroom so the overhead does not push them over budget. The only input operators pick is the disk budget for their stake share; rate and burst both follow from it. The rate must be uniform across validators (see Consensus impact).

### Model — global now, optional per-signer later

**Model 1 (global throughput)** is the base: one per-validator token bucket charging the flat `UploadSize`. The stake-weighted assignment already makes per-node disk bounded and stake-proportional, so a flat global charge needs no explicit weighting and keeps admission uniform. No validator accepts a blob its peers reject. Charging per-validator amounts (Models 2 and 3) breaks that uniformity and risks a partial quorum. Occupancy gating (Model 5) is more precise but heavier.

Model 1 gives no fairness between clients. With a single client that costs nothing, and the global cap bounds disk no matter how many clients appear. **Model 4**, a per-signer sub-limit under the same global cap, waits until real contention shows up. Adding it later is non-breaking, unlike the reject-fast change below: it is a server-side sub-limit and clients see the same `ResourceExhausted` either way, so there is no reason to pay for per-signer state, escrow weighting, and a Sybil policy before they earn their keep. Admission is gated by escrow rather than identity, so if contentious multi-client use is expected early, Model 4 goes into v1 instead.

### Rejection — reject-fast

When an upload is over budget the server returns `ResourceExhausted` with the computed delay as a retry-after hint, and the client waits or routes elsewhere. This avoids the costs of blocking: parked handlers, unpredictable cross-validator latency, and one client stalling others. The client must respect the hint and preserve its quorum, which needs a client change. We make it now, while Fibre has no production users; it is non-breaking today and would be breaking once clients depend on the server absorbing the wait.

Reject-fast does trade away some latency: an over-budget reject forces the client to reroute, which can cascade across busy nodes (raised by Wondertan on the PR). A bounded queue that briefly blocks instead of rejecting would recover that latency near the boundary. It is deferred as a refinement (see Alternative Approaches), because the window-sized burst already absorbs bursts — the two are substitutes, and with a 1/2-window burst the bucket rarely empties, so the queue would seldom engage and would degrade to reject under sustained load anyway.

## Detailed Design

### Mechanism

The limiter guards `UploadShard`. It lives on `ServerConfig` (TOML and CLI flags, `fibre/server_config.go` and `fibre/cmd/start_cmd.go`) and is a no-op when disabled or when `rate ≤ 0`.

- **Bucket.** A `golang.org/x/time/rate` limiter, tokens in bytes, charging `UploadSize` per upload, sized as above.
- **Charge point.** After `verifyPromise`, `verifyAssignment`, and `verifyShard` (`fibre/server_upload.go`), and only when the shard is not already stored. A replay of an accepted promise or shard must not drain the bucket. The order is fixed: presence check, then charge on a miss, then `store.Put`; a hit returns OK and never touches the bucket. This presence check is a prerequisite for the implementation. It relates to ADR-025 but does not depend on it: if ADR-025's promise cache lands, the check can read from it; otherwise a direct cheap store lookup suffices. Either way the charge sits behind the same gate, so a replay cannot drain the bucket regardless of which cache exists. Charge against server time, never the client's `CreationTimestamp`.
- **Reject-fast.** `ResourceExhausted` with a retry-after delay carried in a gRPC `RetryInfo` detail. No parked handlers.
- **Metrics.** A `rate_limited` counter (by reason) and an `admitted_bytes` counter (`fibre/server_metrics.go`). The existing `upload_shard.bytes` counts received shard bytes, distinct from the charged `UploadSize`.

### Transport-level limits

The byte-rate limiter bounds disk, not memory. gRPC buffers each full `UploadShard` message (up to `MaxMessageSize`, about 132 MiB — the 129.83 MiB shard plus the promise and ~2% protobuf framing, so larger than the 128 MiB blob) before the handler runs, so a rejected upload has already been received. Receive memory is bounded only at the transport layer, by `MaxRecvMsgSize × concurrent_streams × connections`, and none of those factors is capped today. This work adds, as gRPC server options plus listener wrapping:

- **`MaxConcurrentStreams`** — cap in-flight RPCs per connection.
- **Maximum concurrent connections** — a limiting listener caps total accepted connections, so a peer cannot sidestep the per-connection stream cap by opening many connections.
- **Keepalive enforcement** (`KeepaliveEnforcementPolicy`, `KeepaliveParams`) — shed idle or abusive connections and blunt slow-read holding.

These bound receive memory and connection load. With them and the verifier pool in place, the earlier application-level in-flight cap is redundant and is dropped. They are concurrency and connection caps, not a rate, so they take fixed protocol-sane values rather than anything derived from the disk budget.

### Configuration and tuning

The disk budget and the enable flag are the config inputs (`ServerConfig`, TOML and CLI); rate and burst are derived from the budget (see Sizing), not set independently. The limit can be raised as usage grows or turned off entirely, with no protocol change. The rate itself is intended to be a governance parameter rather than free per-operator config, so that uniformity holds by construction (see Consensus impact); local config carries the enable flag and, during the ramp-up bridge, the coordinated rate. Because a non-uniform rate can deny quorum (see Consensus impact), the rate is a coordinated network value, not a free per-operator setting; raising it is a coordinated change. The override is asymmetric. Disabling the limiter locally is safe for quorum, since it only risks that node's own disk. Setting a rate lower than one's peers is not safe: it throttles blobs others accept and can push a client below the `2/3` it needs.

**Invariant: `burst ≥ MaxBlobSize`.** The derivation guarantees this by construction — it is the `MaxBlobSize` floor in `burst = max(MaxBlobSize, rate × retention_window / 2)`. It matters because `golang.org/x/time/rate` fails any `ReserveN(n)` or `AllowN(n)` with `n > burst` (returns `OK() == false`), so a `burst` below `MaxBlobSize` (128 MiB) would permanently reject every max-size blob with no error at startup. `ServerConfig` validation (`fibre/server_config.go`) asserts it defensively — guarding a manual override or a future change to the derivation — alongside the `budget > 0` and `UploadVerifyWorkers ≥ 1` checks.

### Optional per-signer sub-limit (Model 4)

Model 4 adds a second bucket keyed on `PaymentPromise.SignerKey`, charged after the global check passes, with per-signer state evicted like ADR-025's cache. Sybil (one client, many signer accounts) is contained twice over. The global bucket still bounds disk regardless of signer count, so Sybil only touches fairness. And sizing each signer's share by its escrow balance ties a client's total share to its total escrow: N accounts need N× the escrow for N× the share. An equal-split policy would instead need a cap on distinct active signers.

### Sizing example

Defaults (`fibre/protocol_params.go`, `x/fibre/types/params.go`): `OriginalRows = 4096`, `MaxRowSize = 32 KiB`, `retention_window = 4h`.

- Max charged `UploadSize = 4096 × 32 KiB = 128 MiB` (= `MaxBlobSize`).
- Max stored shard `MaxShardSize() = 136,134,656 B = 129.83 MiB`: 128 MiB of row data plus 1.83 MiB of overhead (proofs `4096 × 448 B`, RLC `4096 × 16 B = 64 KiB`, indices `4096 × 4 B`). That is the ~1.014 ratio above; actual disk runs about 1.4% over charged bytes.

Worked rate example. Take a `reference_disk_budget` of 1 TiB. The window-burst term dominates `MaxBlobSize`, so `rate = 2 × 1 TiB / (3 × 4h) ≈ 48.5 MiB/s` of charged bytes and `burst = 1 TiB / 3 ≈ 341 GiB`. Peak charged bytes stay at 1 TiB, and the operator provisions about `1 TiB × 1.014 ≈ 1.014 TiB` of disk plus headroom. No budget is fixed yet, so the concrete rate is still open; given a budget it follows from `reference_disk_budget = burst + rate × 4h` with `burst = max(128 MiB, rate × 2h)`.

### Consensus impact

All five models are off-chain. The limiter runs in the Fibre server, outside the ABCI state machine, and only decides which uploads a validator accepts and signs. It does not affect block validity or determinism; validators on different settings still agree on every block and differ only in what they sign. Two interactions matter:

- **Settlement quorum.** `MsgPayForFibre` is validated on-chain against signatures from more than `2/3` of validators (`x/fibre/keeper/msg_server.go`). The limiter does not change that rule, but throttling can stop a client reaching `2/3` and fail the settlement transaction. That liveness effect is why the rate must be uniform: non-uniform limits systematically deny quorum.
- **On-chain rate (governance parameter — recommended).** Because uniformity is a quorum requirement, the rate should be a governance parameter. An `x/fibre` module parameter guarantees every validator uses the same value by construction, instead of relying on operators to coordinate local config where one lagging or misconfigured validator can deny quorum. Promoting it is a normal versioned-upgrade module change, not a fork, and enforcement still lives in the off-chain server. The trade-off is tuning speed, but routine tuning of the rate is not urgent, and the emergency path — disabling locally — stays local and is quorum-safe. burst derives from the rate, so it follows automatically; only the enable flag needs to be local. The recommendation is to make the rate a gov parameter from v1; coordinated local config is acceptable only as a ramp-up bridge (no users yet, low quorum risk) with a commitment to promote.

The consensus-critical kind of limiter, inside `PrepareProposal` / `ProcessProposal` and required to be deterministic across validators, is out of scope.

## Alternative Approaches

The chosen design is Model 1 as the base, optional Model 4 for fairness, reject-fast with a short bounded queue.

**Model 1 — global throughput (chosen base).** One flat `UploadSize` bucket per validator. Bounds per-node disk automatically and stake-proportionally, keeps admission uniform (no partial quorum), simplest to build and remove. Downsides: no client isolation on its own, and low-stake validators are metered by full blob size though they store less.

**Model 2 — shard size × stake.** Charge the bytes each validator actually stores. Most direct disk accounting, but validators then charge different amounts for the same blob, so a blob can be admitted by some and throttled by others: partial-quorum risk and a non-deterministic signer set.

**Model 3 — PFF size × stake.** Uniform charge, per-validator budget scaled by stake. With a single protocol-wide rate this collapses into Model 1, since assignment already makes disk stake-proportional; with genuinely per-validator budgets it reintroduces Model 2's partial-quorum risk. No net gain.

**Model 4 — per client / escrow / signer (chosen hybrid, deferred).** Per-signer, escrow-weighted bucket. Gives real client isolation and can track escrow, but cannot bound total disk alone (still needs the global cap) and adds per-signer state; Sybil is mitigated as above.

**Model 5 — occupancy gating.** Admit on current store size versus budget. The most direct disk enforcement, since a token bucket refills even when nothing is stored while occupancy does not, but it needs live occupancy accounting and back-pressure and is burstier near the cap. A likely later refinement.

**Rejection — reject-fast (chosen), block-and-wait, or hybrid queue.** Block-and-wait needs no client change today but adds head-of-line blocking, unpredictable cross-validator latency, and a contract that is breaking to change later. Reject-fast (chosen) avoids that, at the cost of turning an over-budget upload into a reroute that can cascade across busy nodes. A hybrid — a short bounded queue that blocks while the estimated queueing delay stays below the rerouting cost (`max_queue_size < (RTT + client_overhead) / min_upload_latency`), then rejects — recovers the near-boundary latency. It is deferred: the 1/2-window burst already absorbs bursts, so the queue would rarely engage, and since the charge happens after verify, a waiter holds its full ~132 MiB message and a stream slot, so a bounded queue re-introduces a dedicated wait budget — the in-flight cap this design drops. Revisit if a small burst is chosen or the small-request-near-boundary pattern appears.

**Temporary hardcoded guard.** Rejected. The limiter is permanent, so a throwaway fixed rate (for example a placeholder 10 MiB/s) is wasted work and starves easily; a conservative Model 1 setting covers the ramp-up.

## Consequences

**Positive.** One permanent mechanism for ramp-up and steady state. Disk bounded and derivable from a budget. Uniform charge keeps admission predictable and quorum-safe. Per-signer fairness available later without a redesign. Replay drain closed. Transport caps (streams, connections, keepalive) bound the previously-unbounded receive memory, and remove the need for the old in-flight cap.

**Neutral.** Sizing is independent of the model. Per-node disk scales with stake, a deliberate property (higher stake, higher commission). `admitted_bytes` (charged) and `upload_shard.bytes` (received) measure different things. No proto or consensus change; the limit is raised or disabled by config as usage grows.

**Negative.** Reject-fast needs a client change before clients depend on the limiter. Model 1 alone has no client fairness; that waits on the per-signer layer and its escrow-weighting and Sybil policy. Low-stake validators are metered by full blob size. An over-budget reject costs the client a reroute; a bounded queue to soften that is deferred. The download path stays unthrottled (out of scope).

## References

- PROTOCO-1547 — tracking issue.
- PR #7481 — draft upload admission controller (not merged); the design discussion behind this ADR.
- PR #7489 — pruning window reduced to 4h.
- `x/fibre/types/params.go` — `ShardRetention` (4h), `PaymentPromiseTimeout` (1h).
- ADR-025 — Fibre local promise cache (per-signer accounting, replay mitigation).
- ADR-027 — single-sequencer BFT ordering on Fibre.
- `fibre/validator/set.go` — stake-weighted row assignment.
