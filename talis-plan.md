# Talis + Fibre: Missing Pieces

## How fibre-txsim Actually Works

`fibre-txsim` is a **fibre client** — it does NOT talk to a local fibre server. The architecture is:

```
                    ┌─ validator-0 fibre server (:9091) ──► UploadShard
fibre-txsim ───────┼─ validator-1 fibre server (:9091) ──► UploadShard
  (on validator)    └─ validator-2 fibre server (:9091) ──► UploadShard
       │
       └──► localhost:9091 (celestia-appd gRPC)
              - query validator set + host registry
              - broadcast MsgPayForFibre tx
```

1. `--grpc-endpoint localhost:9091` connects to the **local celestia-appd gRPC** (state queries + tx submission)
2. The fibre `Client` uses a `HostRegistry` to discover each validator's fibre server address on-chain (registered via `talis setup-fibre` / `celestia-appd tx valaddr set-host dns:///IP:9091`)
3. On `Upload()`, it connects **directly to each validator's fibre server** and calls `UploadShard` (the gRPC method)
4. After shards are uploaded and signatures collected, it broadcasts `MsgPayForFibre` via the local celestia-appd gRPC

So `fibre-txsim` does NOT need a local fibre server, but every **validator** must be running the `fibre` server binary to receive `UploadShard` calls.

## Does `talis fibre-txsim` Actually Work?

The talis command (`tools/talis/fibre_txsim.go`) **does** SSH into validators and run `fibre-txsim` in a tmux session via `runScriptInTMux`. The code even has a comment: _"fibre-txsim is already in /bin/ from the payload"_ — but that's incorrect since the binary is never actually deployed.

## fibre-txsim Limitations: Single Account Only

Source: `tools/fibre-txsim/main.go`

fibre-txsim only supports a **single account**. It takes one `--key-name` (default `"validator"`), creates one `user.TxClient` and one `fibre.Client` with that key, and every concurrent goroutine shares them.

The `--concurrency` flag controls parallel blob submissions via a semaphore, but they all use the same signing key/account. This means concurrent submissions will likely hit **sequence number conflicts** — `TxClient.BroadcastTx` needs sequential nonces, and multiple goroutines racing on the same account will clash.

To load-test with higher throughput, you'd need to either:
- Run multiple `fibre-txsim` processes with different `--key-name` values
- Modify the binary to support multiple keys internally

## Full Gap List

1. **No `fibre` server binary in the build pipeline** — `build-talis-bins` doesn't build it. The `fibre/cmd/main.go` binary is completely outside the talis flow.

2. **No `fibre` binary in the payload** — `genesis.go` copies `celestia-appd`, `txsim`, `latency-monitor` into `payload/build/`. No `fibre` binary. (Note: when `--build-dir` is passed, the whole directory is copied — so if `build/fibre` exists it would be included, but `build-talis-bins` doesn't produce it.)

3. **`validator_init.sh` doesn't copy or start the fibre server** — it copies three binaries to `/bin/` (celestia-appd, txsim, latency-monitor) and starts `celestia-appd start`. No `fibre start` anywhere.

4. **No talis command to start the fibre server on validators** — `setup-fibre` only registers host addresses + escrow deposits (on-chain txs). Nothing actually runs `fibre start --home ... --app-grpc-address ...` on the remote nodes.

5. **`fibre-txsim` binary not built/deployed** — not in `build-talis-bins`, not copied in `validator_init.sh`.

6. **`reset.go` doesn't clean up fibre or fibre-txsim** — doesn't kill their tmux sessions or remove their binaries.

7. **No Prometheus metrics on fibre server** — fibre has zero counters/histograms/gauges. Only OTel traces + Pyroscope profiling. No way to build throughput dashboards in Grafana.

## What to Add

| # | Gap | What to add |
|---|-----|-------------|
| 1 | Build `fibre` server binary | Add to `build-talis-bins` or separate Makefile target |
| 2 | Build `fibre-txsim` binary | Add to `build-talis-bins` |
| 3 | Deploy both binaries | Include in `genesis.go` payload + copy in `validator_init.sh` |
| 4 | Start fibre server on validators | New talis command or step in `validator_init.sh` to run `fibre start` in a tmux session |
| 5 | Clean up | Update `reset.go` to kill `fibre` and `fibre-txsim` tmux sessions and remove `/bin/fibre*` binaries |
| 6 | Prometheus metrics on fibre server | Add counters/histograms to `fibre/server.go` + expose `/metrics` HTTP endpoint |
| 7 | ~~Pyroscope in observability stack~~ | Out of scope — revisit later |

## What Already Works (once gaps are filled)

- `talis setup-fibre` — registers host addresses + funds escrow (uses celestia-appd tx commands, no binary needed)
- `talis fibre-txsim` — SSHes into validators and runs `fibre-txsim` in tmux (just needs the binary to be in `/bin/`)
- `talis fibre-throughput` — runs locally, polls RPC to monitor per-block throughput (no changes needed)
- `monitor.sh` — already tracks `fibre-txsim` process for CPU/memory

## Fibre Observability: What's Already Built In

The fibre binary (`fibre/cmd/`) has observability support that just needs backends:

### No Prometheus metrics (needs to be added)
- Fibre has **zero** Prometheus counters/histograms/gauges
- This is the biggest gap for throughput dashboards

### Already built in (out of scope for now)
- **Pyroscope**: `--pyroscope-endpoint` flag, continuous CPU profiling, trace-profile correlation
- **pprof**: `--pprof` flag, mutex/block profiling on `:6060`
- **OTel tracing**: `--otel-endpoint` flag, `UploadShard`/`DownloadShard` spans, 10% sampling

---

## Pre-fill 100 fibre accounts for parallel fibre-txsim workers

### Context

Currently each validator gets 2 keys (`validator` and `txsim`). When `fibre-txsim` runs with `--concurrency N`, all N workers share one account, causing sequence number conflicts. We need to create dedicated accounts so each concurrent worker gets its own key/account.

### Changes

#### 1. Talis genesis: create N fibre accounts per validator

**File: `tools/talis/network.go` — `AddValidator()`**

After the existing `txsim` key creation (line 127-151), add a loop:

```go
// Create fibre accounts for parallel blob submission
for i := 0; i < fibreAccounts; i++ {
    name := fmt.Sprintf("fibre-%d", i)
    key, _, err := kr.NewMnemonic(name, keyring.English, "", "", hd.Secp256k1)
    // ... get pubkey, add to genesis with balance 9999999999999999
}
```

- Add `fibreAccounts int` parameter to `AddValidator()`
- Key naming: `fibre-0`, `fibre-1`, ..., `fibre-99`
- Each gets the same balance as `txsim` (9999999999999999)

**File: `tools/talis/genesis.go`**

- Add `--fibre-accounts` flag (default 100)
- Pass value through to `AddValidator()`

#### 2. Go fibre-txsim: one client per concurrent worker

**File: `tools/fibre-txsim/main.go`**

- Replace `--key-name` with `--key-prefix` (default: `"fibre"`)
- At startup, create `concurrency` client pairs:
  ```go
  for i := 0; i < concurrency; i++ {
      keyName := fmt.Sprintf("%s-%d", keyPrefix, i)
      txClient := user.SetupTxClient(ctx, kr, grpcConn, encCfg, user.WithDefaultAccount(keyName))
      fibreClient := fibre.NewClient(txClient, kr, valGet, hostReg, cfg)
      clients[i] = fibreClient
  }
  ```
- Each goroutine gets assigned a client by index (use atomic counter % concurrency)
- Remove the semaphore pattern; instead each worker runs its own loop

---

## Prometheus Metrics for Fibre Server

### What to instrument

Add `prometheus/client_golang` metrics to the fibre server. All metrics should be labeled with the validator's identity for per-validator dashboards.

### Metrics

**File: `fibre/server_metrics.go` (new)**

```go
package fibre

import "github.com/prometheus/client_golang/prometheus"

type ServerMetrics struct {
    // UploadShard
    UploadShardTotal      *prometheus.CounterVec   // labels: status (success|error)
    UploadShardDuration   *prometheus.HistogramVec  // labels: (none)
    UploadShardBytesTotal prometheus.Counter         // total shard bytes received
    UploadShardRowsTotal  prometheus.Counter         // total rows received
    UploadShardsInFlight  prometheus.Gauge           // currently processing

    // DownloadShard
    DownloadShardTotal    *prometheus.CounterVec   // labels: status (success|error|not_found)
    DownloadShardDuration *prometheus.HistogramVec

    // Store
    StoreBlobsTotal       prometheus.Gauge  // number of blobs in store
    StoreSizeBytes        prometheus.Gauge  // approximate store size

    // Throughput (derived, but useful as a direct gauge)
    UploadBytesPerSecond  prometheus.Gauge  // rolling window upload throughput
}
```

Key metrics for throughput dashboards:
| Metric | Type | Purpose |
|--------|------|---------|
| `fibre_upload_shard_total` | counter | Total upload requests (success/error) |
| `fibre_upload_shard_duration_seconds` | histogram | Upload latency distribution |
| `fibre_upload_shard_bytes_total` | counter | Total bytes received → derive throughput via `rate()` |
| `fibre_upload_shard_rows_total` | counter | Total rows received |
| `fibre_upload_shards_in_flight` | gauge | Concurrent uploads being processed |
| `fibre_download_shard_total` | counter | Total download requests |
| `fibre_download_shard_duration_seconds` | histogram | Download latency distribution |

### Where to instrument

**`fibre/server_upload.go` — `UploadShard()`**
- Increment `UploadShardsInFlight` at entry, decrement in defer
- Record `UploadShardDuration` in defer
- On success: increment `UploadShardTotal{status="success"}`, add bytes to `UploadShardBytesTotal`, add rows to `UploadShardRowsTotal`
- On error: increment `UploadShardTotal{status="error"}`

**`fibre/server_download.go` — `DownloadShard()`**
- Record `DownloadShardDuration` in defer
- On success/error/not_found: increment `DownloadShardTotal` with appropriate label

### HTTP endpoint

**`fibre/cmd/start_cmd.go`** or **`fibre/server.go`**
- Start a `/metrics` HTTP endpoint on a configurable port (e.g. `--metrics-address :9464`)
- Use `promhttp.Handler()` to serve metrics
- Default off; enabled when `--metrics-address` is provided

### Prometheus scrape config

**`observability/docker/prometheus/prometheus.yml`** — add:
```yaml
- job_name: 'fibre-server'
  file_sd_configs:
    - files:
        - /targets/fibre_targets.json
      refresh_interval: 30s
  relabel_configs:
    - source_labels: [node_id]
      target_label: instance
      action: replace
```

**`tools/talis/observability_targets.go`** — generate `fibre_targets.json` with validator IPs on port 9464.

### Grafana dashboard

Create `observability/docker/grafana/dashboards/fibre.json` with panels:
- **Upload throughput** (MB/s): `rate(fibre_upload_shard_bytes_total[1m]) / 1024 / 1024`
- **Upload rate** (shards/s): `rate(fibre_upload_shard_total{status="success"}[1m])`
- **Upload latency p50/p95/p99**: `histogram_quantile(0.99, rate(fibre_upload_shard_duration_seconds_bucket[1m]))`
- **In-flight uploads**: `fibre_upload_shards_in_flight`
- **Error rate**: `rate(fibre_upload_shard_total{status="error"}[1m])`
- All panels filterable by validator instance

---

## Implementation Plan

### Phase 1: Build & Deploy Pipeline (get fibre running on talis at all)

**Step 1 — Build binaries**
- `Makefile`: Add `fibre` server and `fibre-txsim` to `build-talis-bins` target
  ```makefile
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags="ledger" -ldflags="$(LDFLAGS_STANDALONE)" -o build/fibre ./fibre/cmd
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags="ledger" -ldflags="$(LDFLAGS_STANDALONE)" -o build/fibre-txsim ./tools/fibre-txsim
  ```

**Step 2 — Deploy binaries to validators**
- `tools/talis/scripts/validator_init.sh`: Add copies for both binaries
  ```bash
  cp payload/build/fibre /bin/fibre
  cp payload/build/fibre-txsim /bin/fibre-txsim
  ```
- `tools/talis/genesis.go`: If not using `--build-dir`, add explicit `copyFile` calls for `fibre` and `fibre-txsim` binaries (with flags like `--fibre-binary` and `--fibre-txsim-binary`)

**Step 3 — Start fibre server on validators**
- New talis command `start-fibre` (or add to `validator_init.sh`) that runs:
  ```bash
  fibre start --home ~/.celestia-fibre --app-grpc-address localhost:9090
  ```
  in a tmux session named `fibre` on each validator
- When observability is enabled, add metrics flag:
  ```bash
  fibre start \
    --home ~/.celestia-fibre \
    --app-grpc-address localhost:9090 \
    --metrics-address :9464
  ```

**Step 4 — Clean up**
- `tools/talis/reset.go`: Add to cleanup script:
  ```bash
  tmux kill-session -t fibre 2>/dev/null || true
  tmux kill-session -t fibre-txsim 2>/dev/null || true
  ```
  And add `/bin/fibre /bin/fibre-txsim` to the `rm -rf` line

### Phase 2: Multi-Account fibre-txsim (parallel workers without nonce conflicts)

**Step 5 — Pre-fill fibre accounts in genesis**
- `tools/talis/network.go`: Add fibre key creation loop in `AddValidator()`
- `tools/talis/genesis.go`: Add `--fibre-accounts` flag (default 100), pass to `AddValidator()`

**Step 6 — Rewrite fibre-txsim for per-worker accounts**
- `tools/fibre-txsim/main.go`:
    - Replace `--key-name` with `--key-prefix` (default `"fibre"`)
    - Create one `TxClient` + `fibre.Client` per worker at startup
    - Each worker runs its own independent loop (no shared semaphore)
    - Keep stats aggregation via atomics

**Step 7 — Update talis fibre-txsim command**
- `tools/talis/fibre_txsim.go`: Replace `--key-name` flag with `--key-prefix`, update the remote command string accordingly

### Phase 3: Observability — Prometheus Metrics

**Step 8 — Add Prometheus metrics to fibre server**
- New file `fibre/server_metrics.go`: Define all metrics (counters, histograms, gauges)
- `fibre/server_upload.go`: Instrument `UploadShard()` with timing, byte counting, error tracking
- `fibre/server_download.go`: Instrument `DownloadShard()` with timing, error tracking
- `fibre/server.go` or `fibre/cmd/start_cmd.go`: Add `--metrics-address` flag, start `/metrics` HTTP endpoint via `promhttp.Handler()`

**Step 9 — Prometheus scrape config + target generation**
- `observability/docker/prometheus/prometheus.yml`: Add `fibre-server` scrape job targeting `fibre_targets.json` on port 9464
- `tools/talis/observability_targets.go`: Generate `fibre_targets.json` with validator IPs

**Step 10 — Fibre Grafana dashboard**
- Create `observability/docker/grafana/dashboards/fibre.json`
- Panels: upload throughput (MB/s), upload rate (shards/s), latency percentiles, in-flight uploads, error rate
- All filterable by validator instance

**Step 11 — Wire up fibre start with observability flags**
- The talis `start-fibre` command should detect if observability is configured in `config.json`
- If yes, pass `--metrics-address` flag

### Phase 4: Verification

**Step 12 — End-to-end test**
- Run full talis flow: `init --with-observability` → `add` → `up` → `genesis` → `deploy` → `setup-fibre` → `start-fibre` → `fibre-txsim` → `fibre-throughput`
- Verify fibre server starts and receives `UploadShard` calls
- Verify fibre-txsim runs with multiple workers without sequence errors
- Verify Prometheus scrapes fibre metrics (check `:9464/metrics` on a validator)
- Verify Grafana fibre dashboard shows throughput, latency, error rate
- Verify `reset` cleans everything up
