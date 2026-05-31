# Fibre read/write benchmark — BASELINE build (no optimizations)

This worktree is the **baseline** ("old, unoptimized") leg of the fibre read/write
A/B experiment. It is built to be **identical to the optimized setup in every way
except the performance optimizations**, so that any latency/throughput delta is
attributable only to those optimizations.

- **Branch:** `experiment/aws-fibre-baseline-no-opt`
- **Worktree:** `/Users/vladkrintisn/Celestia/celestia-app-baseline`
- **Built:** 2026-05-31 — compile-check only (no full talis bundle yet)

## The two setups

| | Optimized ("new") | Baseline ("old") |
|---|---|---|
| Branch | `experiment/aws-fibre-all-prs` | `experiment/aws-fibre-baseline-no-opt` |
| Worktree | `/Users/vladkrintisn/Celestia/celestia-app-combined` | `/Users/vladkrintisn/Celestia/celestia-app-baseline` |
| Fork point | `a401fab36` (2026-04-13) | `a401fab36` (2026-04-13) |
| Telemetry / talis | yes | yes |
| Chain params + functional + txsim | yes | yes |
| **Perf optimizations** | **yes** | **no** |

## How this baseline was constructed

Rather than cherry-pick ~50 wanted commits onto `a401fab36` (error-prone), the
baseline is the optimized tip with **only the optimization commits reverted**.
This guarantees the *only* delta between the two setups is the optimizations.

```
git worktree add -b experiment/aws-fibre-baseline-no-opt \
    /Users/vladkrintisn/Celestia/celestia-app-baseline experiment/aws-fibre-all-prs
# then revert the 7 optimization commits (newest-first)
```

`git diff experiment/aws-fibre-all-prs..experiment/aws-fibre-baseline-no-opt`
touches only: `pkg/rsema1d/*`, `fibre/store.go`, `tools/fibre-txsim/main.go`,
`go.mod`/`go.sum` (restores `go-ds-pebble`). Net: +350 / −730 lines.

## SKIPPED — the optimizations reverted out of the baseline

These are the *only* difference from the optimized build:

| Revert commit | Original | What it optimized |
|---|---|---|
| `c907813ff` | `e0f7fa9de` feat(fibre): TTL-based CachingClient (#7087) | caches validator set (read path) |
| `bc537edf7` | `e35ccacba` feat(fibre): ConstantValsetClient (#7077) | caches validator set (read path) |
| `3cbd2714b` | `3c5c7f416` perf(rsema1d): reduce computeRLCOrig scheduling overhead (#7075) | RLC encode/decode CPU |
| `1f54da953` | `eb4cf7da7` feat(rsema1d): Coder type with cached RS encoder | reuses RS encoder across calls |
| `5338c63ce` | `4cf343fc8` perf(fibre): Pebble value separation for shard storage (#7063) | shard store I/O |
| `0c7d6ccd5` | `5ff371971` refactor(fibre): direct Pebble API (drops go-ds-pebble) (#7062) | shard store I/O |
| `2ac8d2df6` | `5aed8d967` perf(rsema1d): reuse SHA256 hasher in coefficient derivation (#7064) | hashing CPU on encode |

After revert the baseline confirms:
- `fibre/store.go` back on the `go-ds-pebble` wrapper (no direct Pebble, no value separation)
- `pkg/rsema1d/coder.go` removed — no cached RS `Coder`
- no `CachingClient` / `ConstantValsetClient` — valset fetched per request

> Note: the earlier in-tree micro-opts from the original profiling pass (parallel
> row verification, zero-alloc `hashPair`, inlined `extractSymbols` — see
> `project_fibre_experiment.md`) predate `a401fab36` and are therefore present in
> **both** legs. This A/B isolates only the 7 PRs above, not those.

## INCLUDED on top of `a401fab36` (present in both legs)

`a401fab36` already carried fibre + base telemetry (otel/pyroscope, client/server
metrics) + base talis. On top of that, both legs additionally carry:

**Telemetry / observability**
- `c7160eb81` chore(observability): fibre Grafana dashboard with validator filtering (#7021)
- `698a3f008` feat(fibre): Go runtime metrics for fibre server and txsim (#7135)
- `a8883b87c` fix: shutdown MeterProvider on runtime.Start failure
- `87a4b7853` fix(fibre): strip sample labels before Pyroscope upload (#7140)

**Talis / experiment infra**
- `4d4f7a8d1` feat(talis): dedicated encoder instances for fibre-txsim (#7059)
- `df5bb38eb` / `7bc16e763` feat(talis): AWS (EC2) as a compute provider (#7142)
- `f1551720e` feat(talis): tie S3 payload bucket env vars to the provider (#7144)
- `99a20b475` feat(talis): use local NVMe on AWS i-family instances (#7145)
- `79ec6a5fc` chore(talis): default OS image → Ubuntu 24.04 LTS (#7136)
- `58583f69b` fix(talis): presigned S3 URLs for payload distribution (#7141)
- `3a18d5860` chore: set verify_data=false for blocksync (#7134)

**Chain / consensus params (for A/B parity)**
- `61e92d2a6` feat: upgrade handler sets max square size to 256 (#7076)
- `9d4ac970b` feat: set evidence max age num blocks in upgrade handler (#7067)
- `b8e7b1c08` fix: default slashing params to match mainnet governance (#7090)

**Functional / read-write path (for parity)**
- `c13efbee8` feat!: check RLC when downloading fibre rows (#7041) — read-path verification
- `3a42d9bf8` feat: gas estimation for fibre blobs (#7066)

**fibre-txsim load generator (for identical write-path load)**
- `8bbf4ab5f` feat(fibre-txsim): async tx confirmation for pipelined uploads (#7061)
- `12da242d1` feat(fibre): WithAwaitAllSignatures upload option (#7088)

Plus the usual non-functional carry-over from the stack: dependency bumps, CI
workflows, security hardening (SSH TOFU, Grafana password, zip-slip guard), and
test-flake fixes. These don't affect the measured paths.

## Build / compile-check status (2026-05-31)

| Target | Result |
|---|---|
| `make build-fibre` (celestia-appd +fibre) | ✅ builds |
| `go build ./tools/talis/...` | ✅ builds |
| `go build -tags "ledger fibre" ./fibre/... ./pkg/rsema1d/...` | ✅ builds |
| `go build -tags "ledger fibre" ./fibre/cmd/...` (fibre, fibre-txsim) | ✅ builds |

Reverting the direct-Pebble PRs restored the `go-ds-pebble` dependency in
`go.mod`/`go.sum` and the package still compiles — no later commit hard-depended
on the direct-Pebble API.

## Next step (not yet done)

Produce the deployable talis bundle when ready:
`make build-talis-bins-fibre` in this worktree → upload to S3 (never SCP).
