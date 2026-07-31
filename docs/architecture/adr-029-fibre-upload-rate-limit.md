# ADR 029: Fibre Upload Rate Limiting

## Changelog

- 2026-07-21: Initial draft
- 2026-07-31: Implemented

## Status

Implemented

## Summary

The Fibre server writes every uploaded shard to disk. Nothing today caps how full that disk gets, so a busy or hostile client can grow it without limit. This ADR adds an admission limit to the upload path.

The limit is occupancy-based. Each validator tracks how many bytes of shards it currently holds and refuses new uploads once that reaches a disk budget. The budget is not set per operator: one governance parameter fixes the storage a full-stake validator should hold, and each server derives its own budget from that and its stake. An over-budget upload is rejected right away with a retry-after hint. Because shards are pruned after a retention window (4h by default), occupancy falls as old shards age out, so the budget is a moving ceiling rather than a permanent wall.

The bound is enforced by measuring the disk directly, not by translating a byte rate into a disk figure. A few transport-level caps (concurrent streams, connections, keepalive) bound memory alongside it.

## Context

Fibre is Celestia's off-chain, high-throughput data-availability path. A client erasure-codes a blob and sends each validator the rows assigned to it. Each validator's Fibre server stores its assigned rows (its shard) on local disk.

The only operation that grows this store is the `UploadShard` RPC (`fibre/server_upload.go`), which ends in a `store.Put`. Pruning and reconciliation only delete. Nothing limits how fast uploads arrive, so nothing limits how full the disk gets. A validator has no defense against a client, friendly or not, that uploads faster than planned.

There are limits today, but none bound total bytes to disk. A per-message size cap bounds a single request. A pool of verification workers (`ServerConfig.UploadVerifyWorkers`, default `runtime.GOMAXPROCS`) bounds CPU. The `grpc.NewServer` call (`fibre/internal/grpc/server.go`) sets no `MaxConcurrentStreams`, so a peer can open many streams at once and pin a large amount of receive memory. None of this caps disk.

What saves us is that shards expire. Each is pruned at `max(creation + ShardRetention, ExpiresAt)`: `ShardRetention` is 4h by default and `ExpiresAt` is one hour after creation (`PaymentPromiseTimeout`), so the 4h term dominates. A once-a-minute loop in `fibre/server_prune.go` enforces it. So each shard has a bounded residence time and occupancy always drains, which keeps an occupancy-based limit a live ceiling rather than a one-time fill.

We want this for two reasons. In the near term it lets us run Fibre on mainnet during the 2026 ramp-up without a surprise in disk usage. In the long term validators run on fixed hardware and do not autoscale, so a disk bound is always useful. This is a permanent mechanism, not a throwaway guard.

One question stays open: what should the limiter meter, and at what scope? There are five candidates, referred to by number below.

1. **Global throughput** — one flat token charge per upload, the same for every validator.
2. **Shard size × stake** — charge each validator for the bytes it actually stores.
3. **PFF (PayForFibre) size × stake** — a flat charge, but a per-validator budget scaled by stake.
4. **Per client / escrow / signer** — a second limit keyed on who is paying.
5. **Occupancy gating** — admit based on how full the store is right now.

### Relevant mechanics

Three facts about Fibre shape the design.

- **Assignment is stake-weighted.** A validator with stake fraction `s` is assigned about `3s` of the original rows — at `1/3` stake it gets all `OriginalRows`, clamped to a `MinRowsPerValidator` floor and to the full set (`Set.Assign` in `fibre/validator/set.go`). Bigger validators store more per blob. Disk already scales with voting power, before any limiter.
- **What a validator stores.** For each blob a validator keeps its assigned rows plus per-row proofs, the RLC vector, and row indices — roughly `min(1, 3s) × MaxShardSize`, with the `MinRowsPerValidator` floor for small validators and at most one full shard. Occupancy counts these stored bytes directly, so the limit meters exactly what fills the disk.
- **A client needs a quorum.** A client uploads to every validator and needs signatures covering at least `2/3` of stake (`SafetyThreshold`) to settle on-chain. Its client library (`ClientCache.Request` in `fibre/internal/grpc/client_cache.go`) does not retry application errors. So a rejection must not silently cost the client its quorum.

### Requirements

- **R1** — Bound each node's disk to a budget, directly and at all times, without relying on operators to set it correctly.
- **R2** — New clients or heavier usage must not break correctness or force a redesign. Fairness between clients is nice to have, but separate.
- **R3** — Keep the `2/3` quorum reachable while the limiter is throttling.
- **R4** — Keep it simple: one governance parameter, derived per node, with a CLI flag to disable.

### Out of scope

Two nearby things are excluded. The first is limiting throughput inside block production (`PrepareProposal` / `ProcessProposal`). That path persists through goleveldb and is consensus-critical, so any limit there has to be deterministic across validators — a far larger, separate rework of the block pipeline rather than the off-chain Fibre server, and not near-term. The second is the read path (`DownloadShard`), which never grows the store.

## Decision

### Sizing — derive the budget from stake

The budget is the cap directly; there is no rate to derive. We do not ask each operator to set it, because a validator with plenty of disk that never updates its local number would throttle, or even deny quorum for the whole network. Instead one governance parameter, `FullStakeStorageBudget`, fixes the shard storage a 100%-stake validator should hold, and each server derives its own budget from that and its stake.

Storage is already stake-weighted: a validator at stake `s` is assigned an `assignmentFraction` of the rows (about `min(1, 3s)`, with the `MinRowsPerValidator` floor for the smallest; `Set.Assign`), so it stores that fraction of a full shard per blob. The budget follows the same fraction:

> `budget(v) = FullStakeStorageBudget × assignmentFraction(v)`

This is network-consistent: every server computes the same budget from on-chain stake, so admission stays uniform across validators. The budget is a requirement, not a function of the disk a node happens to have — it tells each operator the minimum storage their stake obliges them to provide. A node that has provisioned at least that much never fills; one that has not overflows its disk, which a startup check catches (see Configuration). Occupancy counts real stored bytes, so `FullStakeStorageBudget` is in plain disk bytes, with no charged-to-stored translation.

Deriving from stake also matches where data lands. A small validator's disk rarely fills: a PFF needs signatures covering `2/3` of stake, so if the large validators are full, the blob is rejected network-wide and the small validator's spare disk goes unused anyway. Sizing its budget down costs nothing and spares small operators from provisioning disk that would not pay off.

The retention window still matters, but only for how fast space frees up, not for the cap. Without pruning, occupancy would only rise and the store would sit permanently at budget. Because each shard prunes after the window, occupancy falls as shards age out, so the store retreats below the budget instead of settling at it. Steady-state throughput is then whatever prunes out per unit time; the network self-regulates without a set rate.

### Model — occupancy now, per-signer maybe later

We use **Model 5**: each validator admits an upload only while its current store size is below its budget (derived from stake as in Sizing).

Occupancy meters disk directly: the budget is the cap, with no rate to derive and no window assumption behind the bound. Because occupancy is read from the store, not held in memory, a restart or crash cannot lose it: the node re-reads its real disk use on startup and keeps the same ceiling.

The cost is that admission is not uniform the way a token bucket's is. Occupancy is per-node state, so near the budget one node can admit a blob another momentarily rejects. Stake-derived budgets keep this mild: fill fractions equalize, so it is mostly timing jitter and does not put the `2/3` quorum at serious risk (see Consensus impact). Models 2 and 3 are worse: they charge structurally different amounts for the same blob, so admission diverges at any occupancy, not only near the cap. A global token bucket (Model 1) avoids divergence but bounds disk only indirectly. We take occupancy because a direct, restart-proof disk ceiling outweighs the mild near-cap jitter.

Model 5 gives no fairness between clients. That is fine now: there is one client, and the budget bounds disk no matter how many clients appear. **Model 4**, a per-signer sub-limit under the same budget, is left for later, when there is real contention to manage. It can be added without breaking anything, because clients see the same rejection either way. If contested multi-client use is expected early, it can ship in v1 instead.

The limiter defends disk, not fairness. The disk bound holds under any load, including a hostile client: the budget is a ceiling that no client, or set of clients, can exceed. What it does not give is client isolation — one client can fill the whole budget and starve the rest. Fairness is deferred to Model 4, and because Fibre accounts are free to create, that layer has to weight each signer's share by its escrow rather than split the budget per account; a per-account split would just invite a Sybil client to spin up more accounts.

### Rejection — reject-fast

When the store is at budget, the server rejects the upload right away with `ResourceExhausted` and a retry-after hint, and the client waits and retries. It does not hold the request open waiting for space.

The retry-after hint is coarser than a rate limiter's. A token bucket refills at a known rate and can say exactly when enough returns; occupancy only falls when shards prune, in once-a-minute batches, so the server estimates: the prune interval, or the time until the oldest shards expire, is a reasonable hint.

Blocking would be worse: it ties up server handlers, spreads unpredictable latency across validators, and lets one client stall others. Reject-fast avoids all of that. It does need a client change (the client must honor the hint and keep its quorum), and we make it now, while Fibre has no production users: harmless today, breaking once clients rely on the server absorbing the wait.

Rejecting has one cost: an over-budget reject sends the client off to retry, which can pile up if the next node is also full. A short queue that blocks briefly instead of rejecting would smooth that out near the boundary. We defer it for two reasons. The store usually sits below budget, so rejection is rare. And when it does reject, freeing space waits on the next prune — up to the prune interval away, too long to park a live handler holding its full message (~132 MiB) and a stream slot, which is the very in-flight cap this design otherwise drops. See Alternative Approaches.

## Detailed Design

### What happens on an upload

For each `UploadShard` request the server:

1. Verifies the payment promise, the assignment, and the shard rows (`verifyPromise`, `verifyAssignment`, `verifyShard` in `fibre/server_upload.go`). A failure here returns an error, unchanged from today.
2. Checks whether the shard is already stored. If it is (a replay), the server returns OK and stops. It does not touch occupancy.
3. Otherwise checks occupancy: if the current store size plus this shard's bytes would exceed the budget, it rejects with `ResourceExhausted` and a retry-after hint. If there is room, it reserves the space by adding the shard's bytes to the count.
4. Stores the shard and signs. If the store fails, it releases the reserved bytes and returns the error.

The check sits between the replay check and a successful store. A replay never reaches it, and a failed store releases the reservation, so space that never reaches disk is not held. Reserving at the check, rather than counting only after the store, keeps two concurrent uploads from both passing and overshooting the budget by more than a single shard. The periodic resync (see below) corrects any residual drift.

### Payment on a rejection

A rejected upload does not cost the client anything, which follows from where the check sits. The server only persists a promise as part of storing the shard (`store.Put`, step 4), the same point occupancy is committed — both after the admission check. So an over-budget upload is turned away before its promise is banked. A validator that rejects has nothing to settle: it cannot push the promise on-chain through the timeout path, because it never stored it, and it never signs, so it never joins the settlement quorum.

Settlement is unchanged. `MsgPayForFibre` settles against the `2/3` signature quorum, so the client pays once the validators that stored and signed cover the threshold — for a blob genuinely available at `2/3` of stake. A rejection risks only the client's quorum (R3), never a payment for a refused upload. On the client side the escrow budget is debited at dispatch and stays debited, because any validator that did store the shard can still settle through the timeout path; that is conservative local accounting, not a payment to the validators that rejected.

Because occupancy is a concrete number, a validator can expose it: a capacity endpoint reporting how much room it has, so a client can avoid an upload that would only be rejected and can observe utilization. That is observability, not part of the disk bound, so it is a follow-up rather than part of v1. (A rate limiter could expose the same hint from its remaining tokens, so the endpoint does not depend on the model.)

### The limiter

- **Budget.** Derived per node from the `FullStakeStorageBudget` governance parameter and the validator's stake (see Sizing), recomputed when stake changes. There is no operator-set budget; the validator's job is to provision at least this much disk.
- **Occupancy counter.** An in-memory byte count of stored shards, seeded from the store's actual on-disk size at startup. Each admitted upload adds its stored size; each prune's bytes come off at the next resync.
- **Seed on startup.** Occupancy is read from the store directory at startup, so a restart resumes at the real disk use, never at zero. There is no in-memory state to lose across a restart or crash — the store on disk is the source of truth.
- **Authoritative resync.** On every prune tick (once a minute, `fibre/server_prune.go`) the counter is reset to the store's real on-disk size, which picks up the prune's deletions and corrects any estimation drift or post-crash discrepancy. Admissions are counted the moment they are reserved, so the counter never sits below true occupancy — the safe direction.
- **Reserve and release.** A pending upload's bytes are added at the admission check and removed if the store fails, so concurrent uploads cannot jointly exceed the budget by more than one shard between resyncs.
- **Presence check.** The admission check is gated on a store-presence lookup so a replay of an accepted shard is not counted twice. `fibre/store.go` today has `Put` and `Get` but no `Has` and no size query, so both the presence lookup and the startup/resync size read are small additions. This relates to ADR-025's promise cache but does not depend on it: if that cache lands, the presence check can read from it; if not, a direct store lookup is enough.
- **Rejection.** `ResourceExhausted` plus a coarse retry-after (prune interval, or time to the oldest shard's expiry) in a gRPC `RetryInfo` detail.
- **Metrics.** Add instruments under the existing `fibre.server.upload_shard.` namespace (`fibre/server_metrics.go`): a `rejected` counter (labeled by reason), plus `occupancy_bytes` and `budget_bytes` observable gauges reporting the current store size and the derived per-node budget. Gauges (not an up/down counter) because occupancy is read from the limiter's live counter on collection, so the reported value cannot drift from a missed increment. These are distinct from the existing `fibre.server.upload_shard.bytes`, which sums per-upload stored shard-row bytes (`len(rows) × row size`) on the success path.
- **Off switch.** Disabled via a CLI flag. Also a no-op when the budget is not positive.

### Transport-level limits

The occupancy limit bounds disk, not memory. gRPC reads a whole `UploadShard` message into memory (up to `MaxMessageSize` ≈ 132 MiB: the 129.83 MiB shard plus the promise and ~2% protobuf framing) before the handler runs, so a rejected upload has already been received. Receive memory is capped only at the transport layer, by `MaxRecvMsgSize × streams × connections`, and only `MaxRecvMsgSize` is bounded today.

This work adds three transport caps, as gRPC server options and a wrapped listener:

- **`MaxConcurrentStreams`** — limit in-flight RPCs per connection.
- **MaxConnections** — a limiting listener caps total connections, so a peer cannot dodge the per-connection stream cap by opening many connections.
- **KeepAlive** (`KeepaliveEnforcementPolicy`, `KeepaliveParams`) — drop idle or abusive connections and blunt slow-read holding.

Together these bound receive memory and connection load. With them plus the verifier pool, the application-level in-flight cap from the draft PR (#7481) is not needed here and is dropped. These are fixed, protocol-sane values, not derived from the disk budget.

### Configuration and tuning

The only tunable is the `FullStakeStorageBudget` governance parameter; every per-node budget is derived from it. Making it governance-changeable, rather than local config, removes the failure mode where a validator has the disk but never updates a local number and quietly throttles the network. Because it is normalized to full stake and scaled per node, one on-chain value fits validators of any stake. The only local control is a CLI flag to disable the limiter, an emergency off-switch; there is no local budget to misconfigure.

Two startup checks matter, rerun on any stake or parameter change. First, the derived budget must cover at least one of the node's own shards, or it rejects everything; at the governance level this means `FullStakeStorageBudget ≥ MaxShardSize` (a full-stake shard), and the per-node form catches the smallest validators, which carry a fixed RLC component that does not shrink with stake. Second, available disk must be at least the derived budget; a node with less has under-provisioned for its stake, and the server warns loudly (or refuses to start) rather than overflowing its disk at runtime.

### Per-signer sub-limit (Model 4)

Model 4 adds a per-signer occupancy check, keyed on `PaymentPromise.SignerKey` and applied after the global budget check passes, with per-signer state evicted like ADR-025's cache. A Sybil client (many signer accounts) is handled on two levels. The global budget still bounds disk whatever the signer count, so Sybil can only affect fairness. And sizing each signer's share by its escrow balance ties a client's total share to its total escrow: N accounts need N times the escrow for N times the share. An equal split would instead need a cap on how many signers can be active.

### Sizing example

Using the defaults (`fibre/protocol_params.go`, `x/fibre/types/params.go`): `Rows = 4096`, row size `= MaxBlobSize / Rows = 32 KiB`, `ShardRetention = 4h`.

- Max blob: `MaxBlobSize = 4096 × 32 KiB = 128 MiB`.
- Max stored shard: `MaxShardSize = 129.83 MiB`, the 128 MiB of rows plus ~1.83 MiB of per-shard overhead (proofs `4096 × 448 B`, RLC `4096 × 16 B`, indices `4096 × 4 B`; `MaxShardSize` in `protocol_params.go`).

A worked budget. Say governance sets `FullStakeStorageBudget = 1 TiB`, the disk a 100%-stake validator devotes to Fibre. A validator at 10% stake has `assignmentFraction = min(1, 0.3) = 0.3`, so its budget is `≈ 307 GiB`; a validator at or above `1/3` stake gets the full `1 TiB`. Each must provision at least that much disk. The node admits uploads until its store reaches that budget, then rejects until pruning drops it back below. Peak disk is the budget by construction — there is no rate or burst to compute. No value is fixed yet, so `FullStakeStorageBudget` is still open.

### Consensus impact

All five models run off-chain. The limiter lives in the Fibre server, outside the ABCI state machine, and only decides which uploads a validator accepts and signs. It does not touch block validity or determinism: validators with different settings still agree on every block and differ only in what they choose to sign. Two things interact with consensus.

- **Quorum.** `MsgPayForFibre` settles against the voting-power threshold (`votingPower >= TotalVotingPower × 2/3` in `fibre/validator/signature_set.go`, summing `val.VotingPower` with integer division, so the check is `>=` a floored threshold), not validator count. A validator that rejects does not sign, so throttling can stop a client from reaching the threshold. Deriving the budget from stake keeps this mild: a validator's storage and its budget scale by the same `assignmentFraction`, so every node's fill fraction is about `load / FullStakeStorageBudget` under shared traffic. Validators saturate together rather than one admitting what another rejects, so what is left near the cap is timing jitter, not structural divergence. The residual risk is physical provisioning: quorum needs `2/3` of stake, and a high-stake validator that has not provisioned its derived budget will overflow its disk and drop out. Governance makes the requirement explicit and on-chain (provision `FullStakeStorageBudget` times your stake) and a startup check surfaces a shortfall before it bites, but neither can force an operator to attach the disk. Admission itself stays uniform, since every node uses the same derived budget whatever disk it has.
- **The budget as a governance parameter.** A single on-chain value, `FullStakeStorageBudget`, normalized to full stake, and each server derives its own budget from it and its stake. One value fits validators of any stake, so the *requirement* is uniform by construction while the *per-node* budget still scales. This is why occupancy can use governance where a raw disk number could not: the parameter is normalized, not a physical disk figure. Enforcement stays entirely in the off-chain server; only the parameter lives on-chain.

The kind of limiter that *is* consensus-critical (one inside `PrepareProposal` / `ProcessProposal`, which must be deterministic across validators) is out of scope.

## Alternative Approaches

The chosen design is Model 5 (occupancy gating), optional Model 4 for fairness, and reject-fast rejection with a bounded queue left as a possible refinement. A global token bucket (Model 1) is the main runner-up.

**Model 5 — occupancy gating (chosen).** Admit while the store is below a disk budget. The most direct disk enforcement, since a token bucket refills even when nothing is stored; restart-proof, because occupancy is read from disk rather than held in memory; and cheap to build — a counter seeded from the store size, moved up on admission and resynced to the real size on each prune. Sizing is one governance parameter, derived per node from stake. Weaknesses: no rate pacing, so it admits as fast as the network allows until the cap and then rejects until pruning frees space; a coarser retry-after; and some near-cap admission jitter. We accept these because the goal is a disk ceiling, which occupancy enforces exactly.

**Model 1 — global token bucket (main alternative).** One flat `UploadSize` bucket per validator, sized from the budget by `rate = (budget − burst) / window` and `burst = max(128 MiB, rate × window / 2)`. Its strengths mirror Model 5's weaknesses: it paces ingestion to a steady rate, gives an exact retry-after, and keeps admission uniform by construction (flat charge, flat rate, shared input), degrading under overload to "reject everywhere, retry later" rather than a split. Its weaknesses are why we did not choose it: it bounds disk only indirectly, through the assumption that a shard lives exactly one window, and its in-memory tokens reset on restart, so a restarted node can admit a fresh burst on top of shards already on disk and transiently exceed the budget by up to `burst` (`budget / 3`). Closing that hole means reading occupancy from disk on startup anyway — at which point occupancy is doing the load-bearing work and Model 5 is the more direct mechanism. Model 1 becomes the right choice if a steady ingestion rate and clean back-pressure turn into requirements.

**Model 2 — shard size × stake.** A per-validator token bucket charged the bytes each validator actually stores. Direct accounting, but validators charge different amounts for the same blob, so admission diverges at any occupancy (a worse version of Model 5's near-cap jitter), risking a split quorum and a non-deterministic signer set.

**Model 3 — PFF size × stake.** A flat charge with a per-validator budget scaled by stake. With a single network rate this collapses to Model 1 (assignment already makes disk stake-proportional). With truly per-validator budgets it brings back Model 2's split-quorum risk. No net gain.

**Model 4 — per client / escrow / signer (chosen, deferred).** A per-signer, escrow-weighted sub-limit. Gives real client isolation, but cannot bound total disk on its own (it still needs the global budget) and adds per-signer state. Deferred until there is contention to justify it.

**Rejection — reject-fast (chosen) vs block-and-wait vs hybrid.** Block-and-wait needs no client change today but adds head-of-line blocking, unpredictable latency, and a contract that is breaking to change later. Reject-fast avoids that, at the cost of turning an over-budget upload into a retry that can cascade across full nodes. A hybrid (a short queue that blocks while the wait stays under the retry cost, then rejects) would recover the near-boundary latency. It is deferred for the reasons in the Rejection section: the store usually sits below budget, and a blocked handler waiting on the next prune re-introduces the dropped in-flight cap. Revisit it if the small-request case shows up in practice.

**Temporary hardcoded guard.** Rejected. The limiter is permanent, so a throwaway fixed rate is wasted work and starves easily. A conservative budget covers the ramp-up just as well.

## Consequences

### Positive

- The disk bound is direct and exact: the store never knowingly exceeds the budget, and the bound does not rest on a window assumption or a hand-picked rate.
- Restart-proof by construction: occupancy is read from the store, so a crash or restart cannot lose the accounting or overshoot the budget.
- Sizing is one governance parameter; each node derives its budget from stake, so operators have nothing to set and cannot misconfigure it.
- Simpler to build and remove than a rate limiter: a counter with an authoritative resync, no rate/burst derivation and no reservation library beyond a one-shard in-flight guard.
- A capacity number falls out naturally, so a client can be told or can query how much room a validator has.
- The replay double-count hole is closed.
- Transport caps bound receive memory, which was unbounded, and remove the need for the draft PR's in-flight cap.

### Neutral

- The budget comes from one governance parameter, normalized to full stake and scaled per node, so a single on-chain value covers validators of any stake.
- Per-node disk scales with stake, which is intended: more stake, more commission.
- Two different meters: the new `occupancy_bytes` gauge is the current store level, while the existing `upload_shard.bytes` counter is a cumulative sum of per-upload stored row bytes.
- The limit is governance-set and derived per node; only a CLI flag to disable it is local.
- The counter is in-memory but seeded from disk on startup and resynced from disk each prune, so drift is bounded and self-healing.
- When many clients hit a full store at once they may retry in sync; jittering the retry-after hint is a possible refinement.

### Negative

- No rate pacing: near the cap the store fills as fast as the network allows and then rejects until pruning frees space. If steady ingestion becomes a requirement, a token bucket (Model 1) layered on top would add it.
- The retry-after hint is coarser than a rate limiter's, because space frees in once-a-minute prune batches rather than continuously.
- Admission can still diverge near the cap from timing jitter; a global token bucket avoids even that by construction.
- Quorum reachability depends on high-stake validators actually provisioning their derived budget; one that has not will overflow its disk and drop out. Governance makes the requirement explicit and a startup check surfaces a shortfall, but neither can force an operator to attach the disk.
- Reject-fast needs a client change before clients depend on the limiter.
- Model 5 alone has no client fairness; that waits on the per-signer layer.
- The download path stays unthrottled (out of scope).

## References

- PROTOCO-2122 — this ADR.
- PROTOCO-1547 — rate-limiter tracking issue.
- PR #7481 — draft upload admission controller (not merged); the design discussion behind this ADR.
- PR #7489 — pruning window reduced to 4h.
- `x/fibre/types/params.go` — `ShardRetention` (4h), `PaymentPromiseTimeout` (1h), `FullStakeStorageBudget` (new; disk a 100%-stake validator devotes to Fibre).
- ADR-025 — Fibre local promise cache (per-signer accounting, replay mitigation).
- ADR-027 — single-sequencer BFT ordering on Fibre.
- `fibre/validator/set.go` — stake-weighted row assignment.
