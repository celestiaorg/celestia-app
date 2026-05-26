# Fibre Server

Standalone binary for the Fibre data availability server.

## Build

```sh
make build-fibre-server
```

The binary is output to `build/fibre`.

## Usage

### Start

```sh
fibre start
```

On first run, initializes `~/.celestia-fibre` with a default TOML config.
Subsequent runs load the existing config.

Override the home directory:

```sh
fibre start --home /path/to/fibre-home
# or
FIBRE_HOME=/path/to/fibre-home fibre start
```

Override config values with flags (flags take precedence over config file):

```sh
fibre start \
  --app-grpc-address 127.0.0.1:9090 \
  --server-listen-address 0.0.0.0:7980 \
  --signer-grpc-address 127.0.0.1:26659
```

### Version

```sh
fibre version
```

## Config

The config file is at `$FIBRE_HOME/server_config.toml` (default `~/.celestia-fibre/server_config.toml`).

Config precedence: **flag > config file > default**.

## Signing

Fibre signs payment promises by connecting to the consensus node's PrivValidatorAPI gRPC endpoint. The node handles its own key management (local key, tmkms, etc.) — fibre just delegates signing to it.

### How it works

1. Fibre connects to the node's PrivValidatorAPI gRPC endpoint (default `127.0.0.1:26659`)
2. Fibre fetches the validator's public key via `GetPubKey` RPC to identify itself in the validator set
3. Payment promises are signed via `SignRawBytes` RPC calls for the server's lifetime

### Setup

The privval gRPC endpoint is enabled by default when running `celestia-appd init` on `127.0.0.1:26659`.

To verify or override, check `config.toml`:

```toml
priv_validator_grpc_laddr = "127.0.0.1:26659"
```

## Transport security (TLS)

The Fibre server↔client gRPC link is **TLS-only** (TLS 1.3, always on, no
plaintext fallback). The server presents a self-signed certificate whose
ephemeral TLS key is endorsed by the validator's **consensus key** (signed via
`SignRawBytes` and embedded in a custom X.509 extension). The client verifies
that the peer's certificate is endorsed by the exact validator it intended to
dial, using the consensus pubkey from the current validator set.

Properties and assumptions:

- **Identity is the consensus key, not the network address.** Verification does
  not inspect SNI/SAN/IP, so a validator may register either an **IP literal or
  a DNS name** as its Fibre host — both work.
- **Server-authenticated only.** There is no client certificate / mTLS.
  `DownloadShard` is intentionally **public** (any reachable peer may read
  shards); uploads remain gated by the payment-promise check. If reads must ever
  be restricted, that requires adding client/app-layer authorization.
- **The certificate is long-lived and re-minted on restart; there is no
  in-process refresh or key rotation.** A Celestia validator's consensus key
  does not rotate, and the TLS key is ephemeral (process memory only), so a
  restart is the only re-issuance path needed.
- **Loopback-only links.** The privval signer gRPC (`--signer-grpc-address`) and
  the app-node gRPC (`--app-grpc-address`) are **not** TLS-protected and assume a
  loopback/host-local endpoint. Do **not** point them at a remote host over an
  untrusted network.

### Rollout

Because TLS is always on with no negotiation, a node on this build **cannot**
speak Fibre gRPC with a plaintext (pre-TLS) peer. Roll out to all Fibre peers
together (coordinated / greenfield cutover); a mixed-version Fibre mesh will
partition. Plaintext tooling (`tools/fibre-txsim`, `tools/rust-fibre-txsim` /
lumina) must be updated to the endorsed-TLS verifier before it can talk to a
TLS-only server.

### Design notes

Why the scheme looks the way it does:

- **Endorsement, not the consensus key as the TLS key.** TLS authentication
  needs the private key to sign every handshake. The validator consensus key is
  held in a separate signer (tmkms/HSM) and must not be in the TLS hot path — and
  signers only expose `SignRawBytes`, not raw TLS signing. So the server uses a
  disposable ephemeral TLS key and the consensus key signs it **once** (via
  `SignRawBytes`) to authorize it. The consensus key is touched only at cert mint.
- **Host-agnostic by design.** Verification pins the validator consensus key, not
  the network location (no SNI/SAN/IP/DNS check). This is why the on-chain host
  registry can use an IP literal *or* a DNS name (or `host:port`) — TLS imposes no
  format constraint; the location is just a routing hint.
- **Long-lived cert, re-minted on restart, no in-process refresh.** A Celestia
  validator's consensus key cannot rotate, and the TLS key is ephemeral, so the
  endorsed identity never changes while the server runs; a restart re-mints it.
- **No chain-ID binding.** The endorsement does not bind the chain ID — the TLS
  layer only proves "this peer is validator V". Chain and data correctness come
  from the chain-bound, consensus-key-signed application messages (payment
  promises, acknowledgements) and on-chain data-availability commitments, so a
  chain binding here would be redundant. Decoupling from the runtime chain ID
  also keeps the client free of a startup-ordering dependency.
- **Endorsement carried in a custom DER X.509 extension.** The endorsement
  signature must reach the client at handshake time, so it rides in the cert. DER
  is canonical (friendly to non-Go verifiers like lumina). The extension OID will
  live under a Celestia-owned IANA PEN; it is a documented placeholder until the
  PEN is registered (PROTOCO-1808).

## Observability

All observability flags are persistent and apply to every subcommand.

### Logging

| Flag | Env | Default | Values |
|---|---|---|---|
| `--log-level` | `FIBRE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `--log-format` | `FIBRE_LOG_FORMAT` | `text` | `text`, `json` |

```sh
fibre start --log-level debug --log-format json
```

### Tracing & Metrics

Fibre exports traces and metrics via OTLP/HTTP to any OpenTelemetry-compatible backend (Grafana Alloy, OTel Collector, Tempo, etc.). Both signals share the same endpoint and are enabled together.

| Flag | Env | Default |
|---|---|---|
| `--otel-endpoint` | `FIBRE_OTEL_ENDPOINT` | *(disabled)* |

```sh
fibre start --otel-endpoint http://localhost:4318
```

OTLP uses separate paths on the same endpoint: `/v1/traces` for traces and `/v1/metrics` for metrics.

**Tracing** — The sampler uses `ParentBased(TraceIDRatioBased(0.1))` — 10% of root spans are sampled, and sampling decisions from upstream services are respected.

W3C TraceContext and Baggage propagators are registered globally, enabling distributed trace context to flow across gRPC and HTTP boundaries.

Resource attributes exported with every trace: `service.name=fibre`, `service.version`, `service.instance.id` (hostname).

**Metrics** — Exported via a periodic OTLP reader. All duration histograms carry a `success` attribute for error rate derivation from `_count`. Exemplars are automatically attached to metric observations, linking metric datapoints to traces — in Grafana, clicking an exemplar on a metric panel opens the corresponding trace.

#### Client metrics

| Metric | Type | Attributes | Description |
|---|---|---|---|
| `fibre.client.upload.in_flight` | UpDownCounter | — | Concurrent uploads |
| `fibre.client.upload.duration` | Histogram (s) | `success`, `blob_size` | Upload latency |
| `fibre.client.upload.bytes` | Counter (By) | — | Total bytes uploaded (original rows with padding) |
| `fibre.client.upload.data_bytes` | Counter (By) | — | Total original data bytes (without padding or coding overhead) |
| `fibre.client.upload.network_bytes` | Counter (By) | — | Total bytes pushed to all validators (includes shard duplication) |
| `fibre.client.upload.signatures_collected` | Histogram | — | Signatures per upload |
| `fibre.client.upload_to.duration` | Histogram (s) | `success`, `blob_size`, `validator_address` | Per-validator upload duration |
| `fibre.client.upload_to.rpc_latency` | Histogram (s) | `success`, `validator_address` | Per-validator RPC network latency |
| `fibre.client.download.in_flight` | UpDownCounter | — | Concurrent downloads |
| `fibre.client.download.duration` | Histogram (s) | `success`, `blob_size` | Download latency |
| `fibre.client.download.bytes` | Counter (By) | — | Total bytes downloaded |
| `fibre.client.download_from.duration` | Histogram (s) | `success`, `validator_address` | Per-validator download duration |
| `fibre.client.download_from.rpc_latency` | Histogram (s) | `success`, `validator_address` | Per-validator RPC network latency |

#### Server metrics

| Metric | Type | Attributes | Description |
|---|---|---|---|
| `fibre.server.upload_shard.in_flight` | UpDownCounter | — | Concurrent UploadShard RPCs |
| `fibre.server.upload_shard.duration` | Histogram (s) | `success`, `upload_size` | UploadShard RPC latency |
| `fibre.server.upload_shard.bytes` | Counter (By) | — | Total bytes received |
| `fibre.server.download_shard.in_flight` | UpDownCounter | — | Concurrent DownloadShard RPCs |
| `fibre.server.download_shard.duration` | Histogram (s) | `success`, `shard_size` | DownloadShard RPC latency |
| `fibre.server.download_shard.bytes` | Counter (By) | — | Total bytes sent |
| `fibre.server.store.put.duration` | Histogram (s) | `success` | Store write latency |
| `fibre.server.store.get.duration` | Histogram (s) | `success` | Store read latency |
| `fibre.server.sign.duration` | Histogram (s) | `success` | Payment promise signing latency |
| `fibre.server.prune.entries` | Counter | — | Total entries pruned |
| `fibre.server.prune.duration` | Histogram (s) | `success` | Prune cycle duration |

#### Grafana dashboard

A pre-built Grafana dashboard is available at [`fibre/dashboards/fibre-dashboards.json`](../dashboards/fibre-dashboards.json).

### Profiling (pprof)

Fibre exposes the standard Go `/debug/pprof` endpoints on an opt-in HTTP server.

```sh
fibre start --pprof                  # listen on localhost:6060 (default)
fibre start --pprof=:7070            # listen on a custom address
```

Available endpoints once enabled:

| Endpoint | Description |
|---|---|
| `/debug/pprof/` | Index of all profiles |
| `/debug/pprof/goroutine` | Stack traces of all goroutines |
| `/debug/pprof/heap` | Heap memory allocations |
| `/debug/pprof/allocs` | Past memory allocations |
| `/debug/pprof/block` | Goroutine blocking events |
| `/debug/pprof/mutex` | Mutex contention |
| `/debug/pprof/profile` | 30-second CPU profile |
| `/debug/pprof/trace` | Execution trace |

Mutex and block profiling are enabled automatically when the pprof server starts (fraction=5, rate=1).

### Continuous Profiling (Pyroscope)

Fibre supports push-based continuous profiling to a [Pyroscope](https://grafana.com/oss/pyroscope/) server. When both tracing and Pyroscope are enabled, pprof goroutine labels are automatically annotated with span IDs for trace-profile correlation in Grafana.

| Flag | Env | Default |
|---|---|---|
| `--pyroscope-endpoint` | `FIBRE_PYROSCOPE_ENDPOINT` | *(disabled)* |
| `--pyroscope-basic-auth-user` | `FIBRE_PYROSCOPE_BASIC_AUTH_USER` | *(none)* |
| `--pyroscope-basic-auth-password` | `FIBRE_PYROSCOPE_BASIC_AUTH_PASSWORD` | *(none)* |

```sh
fibre start --pyroscope-endpoint http://localhost:4040

# with authentication (e.g. Grafana Cloud)
fibre start \
  --pyroscope-endpoint https://profiles-prod-001.grafana.net \
  --pyroscope-basic-auth-user 123456 \
  --pyroscope-basic-auth-password <api-key>
```

Profiles are tagged with `version` and `hostname` for filtering in the Grafana UI.

## Signals

- First `SIGINT`/`SIGTERM`: graceful shutdown
- Second signal: force shutdown
