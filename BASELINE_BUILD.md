# Fibre read/write benchmark — no-optimization BASELINE

This branch is the **unoptimized baseline** for the fibre read/write benchmark.
It is the "old commit" leg of the comparison: full telemetry + talis experiment
infra and the same chain params / functional behavior as a current build, but
with **every fibre performance optimization removed** — so any latency/throughput
delta against an optimized (main-based) build is attributable to the
optimizations alone.

- **Branch:** `experiment/aws-fibre-baseline-no-opt`
- **Worktree:** `/Users/vladkrintisn/Celestia/celestia-app-baseline`
- **Base commit:** `a401fab36` (2026-04-13) — already carries fibre + telemetry + talis
- **Built:** 2026-05-31 — compile-check only (no full talis bundle yet)

## How it was constructed

`a401fab36` already has fibre, base telemetry (otel/pyroscope, client/server
metrics), and base talis. Rather than cherry-pick ~50 commits onto it, the branch
was forked from the assembled experiment tip (`experiment/aws-fibre-all-prs`,
itself `a401fab36` + the fibre PR stack) and then had the optimizations removed:

1. **Reverted** the 7 optimization commits from the PR stack (see below).
2. **Stripped** an optimization baked into `a401fab36` itself that the reverts
   couldn't catch (Pebble value separation).
3. **Added** the fibre-reader read-path simulator (PR #7221), adapted for no-opt.
4. **Reconfigured** talis AWS to c7i.8xlarge + a large provisioned gp3 root volume.

## What is NOT optimized (the point of this baseline)

### Storage
- **Pebble value separation + memtable tuning — stripped.** `NewPebbleStore` shipped
  value separation (>4 KB values → blob files) and a 16 MiB memtable *since the
  original fibre import*; this predates the PR stack so the reverts didn't touch it.
  Now runs **default Pebble options** (commit `35e670065`).
- **Direct-Pebble API (#7062) + value separation (#7063) — reverted.** Store is back
  on the `go-ds-pebble` wrapper.
- **Flat-file / raw-file shard store (#7190) — absent.** Never in this branch's
  ancestry; shard `Put`/`Get` go through go-datastore (pebble), no `os.WriteFile`
  bypass.

### gRPC
- **Zero-allocation gRPC codec (#7191) — never added.** No scatter/zero-copy marshal
  path; `fibre/internal/grpc` has only `ClientCache` (basic per-validator connection
  reuse, not a perf optimization).

### rsema1d / CPU
- `#7064` SHA256 hasher reuse, cached-RS `Coder` type, `#7075` computeRLCOrig
  scheduling — all reverted.

### Validator-set caching (read path)
- `#7077` ConstantValsetClient and `#7087` TTL CachingClient — reverted. The
  fibre-reader was adapted to use the default **uncached** state client.

#### The 7 reverted optimization commits

| Revert | Original | Area |
|---|---|---|
| `c907813ff` | `e0f7fa9de` TTL CachingClient (#7087) | valset cache (read) |
| `bc537edf7` | `e35ccacba` ConstantValsetClient (#7077) | valset cache (read) |
| `3cbd2714b` | `3c5c7f416` reduce computeRLCOrig overhead (#7075) | rsema1d CPU |
| `1f54da953` | `eb4cf7da7` Coder type w/ cached RS encoder | rsema1d CPU |
| `5338c63ce` | `4cf343fc8` Pebble value separation (#7063) | storage |
| `0c7d6ccd5` | `5ff371971` direct Pebble API (#7062) | storage |
| `2ac8d2df6` | `5aed8d967` reuse SHA256 hasher (#7064) | hashing CPU |

> **Caveat — still present:** three micro-opts from the original profiling pass
> (parallel row verification, zero-alloc `hashPair`, inlined `extractSymbols`)
> predate `a401fab36` and are CPU-path, not storage/gRPC. They remain in this
> baseline. Remove them too if a fully-naive CPU path is wanted.

## What IS included (parity with an optimized build)

`a401fab36` base + the fibre PR stack minus optimizations:

- **Telemetry:** Grafana dashboard (#7021), Go runtime metrics (#7135),
  MeterProvider shutdown fix, Pyroscope label strip (#7140)
- **Talis:** dedicated encoder instances (#7059), AWS EC2 provider (#7142),
  provider-tied S3 env (#7144), NVMe-on-i-family (#7145), Ubuntu 24.04 (#7136),
  presigned S3 URLs (#7141), verify_data=false (#7134)
- **Chain params:** max square size 256 (#7076), evidence max age (#7067),
  mainnet slashing (#7090)
- **Functional read/write path:** RLC-check-on-download (#7041), gas estimation (#7066)
- **fibre-txsim load gen:** async confirm (#7061), WithAwaitAllSignatures (#7088)
- **Reader (PR #7221):** fibre-reader read-path simulator + talis Reader instance
  type + reader payload/deploy path. Adapted for no-opt (dropped `WithCachedValset`).

## talis AWS instance + volume config

Changed from the upstream i4i NVMe setup to (resolved in `tools/talis/aws.go`):

- **Instance type (all roles — validator/fibre server, encoder, reader):** `c7i.8xlarge`
  (32 vCPU / 64 GiB, EBS-only, up to 12.5 Gbps).
- **Root gp3 EBS:** 1 TB (`AWSDefaultRootVolumeGB=1000`), provisioned to gp3 max —
  **1000 MB/s throughput, 16000 IOPS**. c7i has no local instance-store, so fibre/
  celestia state lives on this root volume; the provisioning keeps the disk out of
  the `store_put` critical path.

> Note: `main` carries an upstream equivalent of the gp3 provisioning
> (`#7192 feat(talis): provision gp3 root volume to max IOPS/throughput`); this
> branch uses a hand-written version of the same idea.

## Build / compile-check status (2026-05-31)

| Target | Result |
|---|---|
| `make build-fibre` (celestia-appd +fibre) | ✅ builds |
| `go build ./tools/talis/...` (new aws.go) | ✅ builds |
| `go build -tags "ledger fibre" ./fibre/... ./pkg/rsema1d/...` | ✅ builds |
| `go build -tags "ledger fibre" ./fibre/cmd/...` (fibre, fibre-txsim) | ✅ builds |
| `go build -tags "ledger fibre" ./tools/fibre-reader` | ✅ builds |

## Next step (not yet done)

Produce the deployable talis bundle: `make build-talis-bins-fibre` in this
worktree → upload to S3 (never SCP).
