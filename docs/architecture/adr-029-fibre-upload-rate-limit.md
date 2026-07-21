# ADR 029: Fibre Upload Rate Limiting

## Changelog

- 2026-07-21: Initial draft

## Status

Proposed

## Context

Fibre is the off-chain, high-throughput DA path. A client erasure-codes a blob and
fans its rows out to validators; each validator's Fibre server receives its assigned
shard and writes it to local disk. The only ingress that grows that store is
`Server.UploadShard` (`fibre/server_upload.go:22` → `store.Put`, `:83`); prune and
reconcile only delete.

Nothing bounds upload throughput or disk growth today. The existing limits are
per-message size (`fibre/server.go:114-115`) and a configurable verifier pool
(`ServerConfig.UploadVerifyWorkers`, default `GOMAXPROCS`; wired at
`fibre/server.go:64`); no `MaxConcurrentStreams` is set on the `grpc.NewServer`
call (`fibre/internal/grpc/server.go:43`), so receive-buffer memory is effectively
unbounded.

Shards expire. `pruneAt = max(creation + ShardRetention, ExpiresAt)`
(`fibre/server_upload.go:183`), where `ShardRetention = 4h` and
`ExpiresAt = creation + PaymentPromiseTimeout` with `PaymentPromiseTimeout = 1h`
(`x/fibre/types/params.go:35`, `:28`; `x/fibre/keeper/keeper.go:329`), so effective
retention is 4h; both are governance-tunable. A minute-interval loop enforces it
(`fibre/server_prune.go:8`). So **steady-state disk ≈ admitted rate × retention
window** — bounding the rate bounds the disk.

We need this for the 2026 ramp-up (run on mainnet without surprise disk growth) and
permanently (validators have fixed hardware and do not autoscale). It is a permanent
mechanism; a conservative early setting is just one operating point.

The open question is what the limiter meters and at what scope: (1) global network
throughput; (2) shard size × stake; (3) PFF (PayForFibre) / `UploadSize` size ×
stake; (4) per client / escrow / signer; (5) something else, e.g. occupancy gating.
How the number itself is chosen (sizing) is a separate axis.

### Relevant mechanics

- **Assignment is stake-weighted.** `Set.Assign` gives each validator
  `clamp(ceil(3s · originalRows), MinRowsPerValidator, originalRows)` rows for stake
  fraction `s` at `livenessThreshold = 1/3` (`fibre/validator/set.go:68`). A
  validator's shard, and its disk per blob, is proportional to its voting power.
- **Charge basis.** `UploadSize = originalRows × rowSize` (`fibre/blob.go:122`) is
  the padded original blob size, uniform across validators. A validator actually
  stores `min(1, 3s) × UploadSize` (its rows, proofs, RLC) — at most ~1× for
  `s ≥ 1/3`, not 4×; the 4× parity is a network aggregate, never on one node.
- **Client.** A client uploads to all validators and needs `>2/3` signatures.
  `ClientCache.Request` does not retry application errors
  (`fibre/internal/grpc/client_cache.go:117`), so rejections must not silently cost
  quorum.

### Requirements

- **R1** Bound per-node disk to a derivable budget over the retention window.
- **R2** New clients or higher usage must not break correctness or force a redesign.
  Fair isolation between clients is desirable but a separate question.
- **R3** Keep the `>2/3` quorum reachable under throttling.
- **R4** Simple, config-gated, tunable, removable — no protocol change.

### Out of scope

Throughput limiting in PrepareProposal/ProcessProposal (a goleveldb concern, and
goleveldb is not near-term) and the read path (`DownloadShard`).

## Decision

### Sizing — from a disk budget

The rate is a single protocol-wide value derived from a reference disk budget, not
hand-picked. A token bucket admits at most `burst + rate × T` over any interval `T`,
and shards live for the retention window, so per-node peak disk is:

> `peak_disk = min(1, 3s) × (burst + rate × retention_window)`

For the reference (max-stake) validator this equals `reference_disk_budget`, giving
`rate = (reference_disk_budget − burst) / retention_window`, with
`burst ≥ MaxBlobSize` so any single blob fits. When `burst` is small relative to the
budget, `rate ≈ reference_disk_budget / retention_window`. Operators do not pick the
rate; they provision disk for their stake share. A single uniform rate across
validators is required (see Consensus impact).

### Model — global now, optional per-signer later

**Model 1 (global throughput)** is the base: one per-validator token bucket charging
the flat `UploadSize`. It is chosen because per-node disk is *already* bounded and
stake-proportional by the stake-weighted assignment, so a flat global charge needs no
explicit weighting and keeps admission uniform — a validator cannot accept a blob its
peers reject. Charging per-validator amounts (Models 2 and 3) breaks that uniformity
and risks partial quorum; occupancy gating (Model 5) is more precise but heavier.

Model 1 gives no client fairness on its own, but with a single client today there is
nothing to protect against, and the global cap already guarantees the disk bound
regardless of client count. **Model 4** — a per-signer sub-limit under the same global
cap — is deferred until contention appears. Unlike the reject-fast client change,
adding it later is *non-breaking*: it is a transparent server-side sub-limit (clients
see the same `ResourceExhausted`), so there is no reason to pay its per-signer state,
escrow-weighting, and Sybil-policy cost before it is needed. Admission is gated by
escrow, not identity, so if contentious multi-client use is expected early, Model 4
moves into v1. Trade-offs for all models are in Alternative Approaches.

### Rejection — reject-fast, not block-and-wait

On over-budget, return `ResourceExhausted` with the computed delay as a retry-after
hint; the client waits or routes elsewhere. This avoids head-of-line blocking and
needs a client change (respect the hint, keep quorum). We make that change now: it is
non-breaking while Fibre has no users, and would be breaking once clients depend on
the server absorbing the wait.

## Detailed Design

### Mechanism

The limiter guards `UploadShard`, configured on `ServerConfig` (TOML + CLI flags,
`fibre/server_config.go`, `fibre/cmd/start_cmd.go`); a no-op when disabled or
`rate ≤ 0`.

- **Bucket.** `golang.org/x/time/rate`, tokens = bytes, charge = `UploadSize`, sized
  as above.
- **Charge point.** After `verifyPromise`/`verifyAssignment`/`verifyShard`
  (`fibre/server_upload.go:142`, `:194`, `:225`), and only if the shard is not
  already stored — a replay of an accepted promise/shard must not drain the bucket.
  The bucket is charged strictly *after* a passing store-presence check, so the
  ordering is fixed: presence check → (miss) → charge → `store.Put`; a hit returns OK
  and never touches the bucket. This presence check is a prerequisite for this ADR's
  implementation. It relates to ADR-025 but does not depend on it: if ADR-025's
  promise cache lands, the store check may be served from it; if not, a direct cheap
  store lookup suffices. Either way the charge is gated on the same check, so a replay
  cannot drain the bucket regardless of which cache is present. Account against server
  time, never the client's `CreationTimestamp`.
- **Reject-fast.** `ResourceExhausted` plus a retry-after (a gRPC `RetryInfo`
  detail); no parked handlers.
- **Metrics.** `rate_limited` (by reason) and `admitted_bytes` counters
  (`fibre/server_metrics.go`); the existing `upload_shard.bytes` counts received
  shard bytes, distinct from charged `UploadSize`.

### Transport-level limits

The byte-rate limiter bounds disk, not memory. gRPC buffers each full `UploadShard`
message (up to `MaxMessageSize ≈ 132 MiB`) before the handler runs, so a rejected
upload has already been received; receive memory is bounded only at the transport
layer, by `MaxRecvMsgSize × concurrent_streams × connections`, and today none of those
factors is capped (`fibre/internal/grpc/server.go:43` sets no such options). This work
adds, as gRPC server options and listener wrapping:

- **`MaxConcurrentStreams`** — cap in-flight RPCs per connection.
- **Max concurrent connections** — cap total accepted connections (a limiting
  listener), so a peer cannot reopen the per-connection cap arbitrarily many times.
- **Keepalive enforcement** (`KeepaliveEnforcementPolicy` / `KeepaliveParams`) — shed
  idle or abusive connections and blunt slow-read holding.

These bound receive memory and connection/handshake load — a companion to the disk
bound, not a substitute. With them plus the verifier pool, no separate
application-level in-flight cap is needed. They are concurrency/connection caps, not a
rate, so they are set to fixed protocol-sane values rather than derived from the disk
budget.

### Configuration and tuning

Rate, burst, and the enable flag are config values (`ServerConfig`, TOML + CLI), so
the limit can be raised as usage grows or disabled entirely, with no protocol change.
Because a non-uniform rate can deny quorum (see Consensus impact), it is a coordinated
network value, not a free per-operator setting — raising the network limit is a
coordinated change. The override asymmetry matters: disabling locally is safe for
quorum (it only risks that node's own disk), but a rate set *lower* than peers
throttles blobs others accept and can deny the `>2/3` quorum.

**Required invariant — `burst ≥ MaxBlobSize`.** `golang.org/x/time/rate` silently
fails any `ReserveN(n)`/`AllowN(n)` where `n > burst` (`OK()` is false), so a `burst`
configured below `MaxBlobSize` (128 MiB) would *permanently reject every max-size blob*
rather than rate-limit it — a hard breakage with no runtime error. `ServerConfig`
validation (`fibre/server_config.go`) must enforce `burst ≥ MaxBlobSize` at startup and
reject the config otherwise, alongside the existing `rate ≥ 0` / `UploadVerifyWorkers ≥ 1`
checks.

### Optional per-signer sub-limit (Model 4)

A second bucket keyed on `PaymentPromise.SignerKey`, charged after the global check,
with per-signer state evicted like ADR-025's cache. Sybil (one client, many signer
accounts) is contained on two levels: the global bucket still bounds disk regardless
of signer count, so Sybil only touches fairness; and sizing each signer's share by
its escrow balance makes a client's total share track its total escrow — N accounts
need N× the escrow for N× the share. An equal-split policy would instead need a cap on
distinct active signers.

### Sizing example

Defaults (`fibre/protocol_params.go:59`, `x/fibre/types/params.go`):
`OriginalRows = 4096`, `MaxRowSize = 32 KiB`, `retention_window = 4h`.

- Max charged `UploadSize = 4096 × 32 KiB = 128 MiB` (= `MaxBlobSize`).
- Max stored shard `MaxShardSize() = 136,134,656 B = 129.83 MiB` — 128 MiB of row
  data plus 1.83 MiB overhead (proofs `4096 × 448 B`, RLC `4096 × 16 B = 64 KiB`,
  indices `4096 × 4 B`).

No disk budget is set today, so the numeric rate is open. Given a budget and
`burst ≥ 128 MiB`, `rate = (budget − burst) / 4h`.

### Consensus impact

All five models are off-chain: the limiter runs in the Fibre server, outside the ABCI
state machine, and only decides which uploads a validator accepts and signs. It does
not affect block validity or determinism — validators with different settings still
agree on every block, differing only in what they sign. Two interactions matter:

- **Settlement quorum.** `MsgPayForFibre` is validated on-chain against `>2/3`
  validator signatures (`x/fibre/keeper/msg_server.go:150`, `:340`). The limiter does
  not change that rule, but throttling can stop a client reaching `2/3` and fail the
  settlement tx — a liveness effect, and the reason the rate must be uniform across
  validators (non-uniform limits systematically deny quorum).
- **On-chain rate (optional).** Making the rate an `x/fibre` parameter for guaranteed
  uniformity is a normal versioned-upgrade module change, not a fork; enforcement
  still lives in the off-chain server.

The genuinely consensus-critical limiter — in PrepareProposal/ProcessProposal, which
must be deterministic across validators — is out of scope.

## Alternative Approaches

Chosen: Model 1 as the base, optional Model 4 for fairness, reject-fast. Trade-offs:

**Model 1 — global throughput (chosen base).** One flat `UploadSize` bucket per
validator. Bounds per-node disk automatically and stake-proportionally, keeps
admission uniform (no partial quorum), and is simplest to build and remove. Downsides:
no client isolation alone, and low-stake validators are metered by full blob size
though they store less (conservative).

**Model 2 — shard size × stake.** Charge the actual stored bytes per validator. Most
direct disk accounting, but validators then charge different amounts for the same
blob, so a blob can be admitted by some and throttled by others — partial-quorum risk
and a non-deterministic signer set.

**Model 3 — PFF size × stake.** Uniform charge, per-validator budget scaled by stake.
With a protocol-wide rate this *is* Model 1 (assignment already makes disk
stake-proportional); with per-validator budgets it reintroduces Model 2's
partial-quorum risk. No net gain.

**Model 4 — per client / escrow / signer (chosen hybrid).** Per-signer bucket,
escrow-weighted. Gives real client isolation and can track escrow, but does not bound
total disk alone (needs the global cap) and adds per-signer state; Sybil is mitigated
as described above.

**Model 5 — occupancy gating.** Admit on current store size vs budget. The most direct
disk enforcement (a token bucket refills even when nothing is stored), but needs live
occupancy accounting and back-pressure and is burstier near the cap. A possible later
refinement.

**Rejection — reject-fast vs block-and-wait.** Reject-fast avoids head-of-line
blocking and gives the client an actionable retry-after, at the cost of a client
change made now while non-breaking. Block-and-wait needs no client change today but
adds HOL blocking, unpredictable cross-validator latency, and entrenches a contract
that is breaking to change later.

**Temporary hardcoded guard.** Rejected: the limiter is permanent, so a fixed
throwaway rate is wasted work; a conservative Model 1 setting covers the ramp-up.

## Consequences

**Positive.** One permanent mechanism for ramp-up and steady state; disk bounded and
derivable from a budget; uniform charge keeps admission predictable and quorum-safe;
per-signer fairness available later without redesign; replay drain closed, and
transport caps (streams, connections, keepalive) bound the previously-unbounded
receive memory.

**Neutral.** Sizing is independent of the model. Per-node disk scales with stake — a
deliberate property (higher stake, higher commission). `admitted_bytes` (charged) and
`upload_shard.bytes` (received) are different metrics. No proto or consensus change;
the limit is raised or disabled by config as usage grows.

**Negative.** Reject-fast needs a client change before clients depend on the limiter.
Model 1 alone has no client fairness — that waits on the per-signer layer and its
escrow-weighting / Sybil policy. Low-stake validators are metered by full blob size.
The download path stays unthrottled (out of scope).

## References

- PROTOCO-1547 — tracking issue.
- `x/fibre/types/params.go` — `ShardRetention` (4h), `PaymentPromiseTimeout` (1h).
- ADR-025 — Fibre local promise cache (per-signer accounting, replay mitigation).
- ADR-027 — Single sequencer BFT ordering on Fibre.
- `fibre/validator/set.go:68` — stake-weighted row assignment.
