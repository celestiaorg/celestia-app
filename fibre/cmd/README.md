# Fibre Server

Standalone binary for the Fibre data availability server.

## Prerequisites

Before starting, make sure:

- [ ] A `celestia-appd` node runs on the same host (or a trusted host-local network). The server's app link (`--app-grpc-address`) and signer link (`--signer-grpc-address`) are **not** TLS-protected — see [Transport security](#transport-security-tls).
- [ ] The chain is on **app version 10 or later**. The `x/fibre` and `x/valaddr` modules the server depends on do not exist in earlier versions.
- [ ] The node's application gRPC endpoint is enabled (default `127.0.0.1:9090`).
- [ ] The node's privval gRPC endpoint is enabled — see [Signing](#signing). If the consensus key lives in an external KMS, the KMS must support the privval `SignRawBytes` message; see the [release notes](../../docs/release-notes/release-notes.md) for the KMS policy.
- [ ] The fibre listen port (default `7980`) is reachable by clients from outside your network.
- [ ] Your validator is bonded. The server derives its storage budget from your stake; a validator outside the active set gets no budget and no traffic.

## Install

### Prebuilt binary

Every celestia-app release attaches a `fibre` archive for Linux and macOS on both
`amd64` and `arm64`. The version matches the celestia-app release it ships with.

The archives are named `fibre_{Linux,Darwin}_{x86_64,arm64}.tar.gz`. Note that a
Linux arm64 host reports `aarch64` from `uname -m`, but the archive is `arm64`.

```sh
curl -LO https://github.com/celestiaorg/celestia-app/releases/latest/download/fibre_Linux_x86_64.tar.gz
curl -LO https://github.com/celestiaorg/celestia-app/releases/latest/download/checksums.txt
# on macOS: shasum -a 256 --ignore-missing --check checksums.txt
sha256sum --ignore-missing --check checksums.txt
tar -xvf fibre_Linux_x86_64.tar.gz
./fibre version
```

The Linux archives are dynamically linked and require **glibc >= 2.34**, so
Ubuntu 22.04 and Debian 12 work. This is lower than the **glibc >= 2.38** floor
of the multiplexer `celestia-appd` build, which comes from its embedded binaries;
fibre embeds none.

### Build from source

```sh
make build-fibre-server
```

The binary is output to `build/fibre`, stamped with the version from
`git describe` and the short commit hash, so `fibre version` reports something
real rather than `dev`. A release build stamps the release tag and the full
commit hash instead.

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
  --signer-grpc-address 127.0.0.1:26669
```

### Version

```sh
fibre version
```

## Config

The config file is at `$FIBRE_HOME/server_config.toml` (default `~/.celestia-fibre/server_config.toml`).

Config precedence: **flag > config file > default**.

### Shard retention

How long uploaded shards are kept locally before pruning is the `shard_retention` on-chain parameter of the `x/fibre` module (default `4h`), changeable by governance. It is independent of the chain's payment-promise timeout. The server reads the current value from the app node on every upload (returned by `ValidatePaymentPromise`), so there is no local setting to configure.

## Signing

Fibre signs payment promises by connecting to the consensus node's PrivValidatorAPI gRPC endpoint. The node handles its own key management (local key, tmkms, etc.) — fibre just delegates signing to it.

### How it works

1. Fibre connects to the node's PrivValidatorAPI gRPC endpoint (default `127.0.0.1:26669`)
2. Fibre fetches the validator's public key via `GetPubKey` RPC to identify itself in the validator set
3. Payment promises are signed via `SignRawBytes` RPC calls for the server's lifetime

### Setup

The privval gRPC endpoint is enabled by default when running `celestia-appd init` on `127.0.0.1:26669`.

To verify or override, check `config.toml`:

```toml
priv_validator_grpc_laddr = "127.0.0.1:26669"
```

## Registration

A running server alone receives no traffic: fibre clients discover servers through the on-chain [`x/valaddr`](../../x/valaddr/README.md) registry and only dial hosts registered there. Once the server is up, register its publicly reachable address, signing with your validator's account key:

```sh
celestia-appd tx valaddr set-host 203.0.113.7:7980 --from <validator-account-key>
```

The host must be in `host:port` form — an IP literal or a DNS name both work (TLS identity is bound to your consensus key, not the network address; see [Transport security](#transport-security-tls)). Port range is [1, 65535], the whole string is capped at 100 characters, and schemes (`http://`, `dns:///`) or URL paths are rejected.

Verify the registration (your consensus address is printed by `celestia-appd comet show-address`):

```sh
celestia-appd query valaddr provider <celestiavalcons-address>
```

`celestia-appd query valaddr providers` lists the registered hosts of all currently bonded validators — yours should appear there once you are bonded.

### When to register

- `set-host` is only accepted once the chain runs app version 10; before the v10 upgrade activates, the `x/valaddr` module does not exist and the transaction is rejected.
- Prepare everything else — install the binary, configure the signer links, verify the KMS supports `SignRawBytes` — before the upgrade, then start the server and register once v10 is live.
- Start the server **before** registering: a registered-but-unreachable host makes clients dial and time out against you.
- Registration is persistent. Re-run `set-host` only when the address changes. The entry is garbage-collected automatically if your validator permanently leaves the set (removed from staking, or jailed and unbonded for over 7 days) — after coming back, register again.

## Transport security (TLS)

The Fibre server↔client link is always TLS-encrypted, and it is fully
automatic: there are no certificates to obtain, configure, or renew.

- On startup the server generates its own certificate and has it endorsed once
  by your validator's consensus key (through the signer). Clients verify that
  endorsement against the validator set, so the connection proves it belongs to
  your validator — no certificate authority involved.
- Identity is bound to the consensus key, not the network address, so the host
  you register on-chain can be an IP literal or a DNS name.
- A restart generates a fresh certificate automatically. After changing the
  signer (`--signer-grpc-address`), restart the server so the certificate is
  endorsed with the right key.
- Downloads are public — any peer can read shards. Uploads are still gated by
  the payment-promise check.

Two things to keep in mind:

- The app link (`--app-grpc-address`) and signer link (`--signer-grpc-address`)
  are **not** TLS-protected. Keep them on the same host or a trusted local
  network. If you need to run fibre on a separate server, use its private IP or a closed network connection.
- There is no plaintext fallback, so every Fibre server and client on the
  network must run a TLS-capable build.

For the full design (endorsement scheme, certificate format, OIDs), see the
[Fibre server spec](../../specs/src/fibre_server.md).

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

## Troubleshooting

### `starting server: creating signer: ...`

The server could not reach the node's privval gRPC endpoint at startup and exits. Check that the node is running, that `priv_validator_grpc_laddr` is set in the node's `config.toml`, and that `--signer-grpc-address` (or `signer_grpc_address` in `server_config.toml`) points at it.

### Warning: `derived storage budget is 0 (validator not in the active set?)`

The server started, but the app node reports no stake for your validator. Either the validator is not bonded, or `--app-grpc-address` points at a node that is still syncing (or at the wrong network). The server keeps running without a storage limit and re-derives the budget periodically.

### Server runs but no uploads arrive

Clients only dial registered, bonded validators. In order:

1. `celestia-appd query valaddr provider <celestiavalcons-address>` — if `found: false`, [register](#registration).
2. Check the registered `host:port` actually routes to this server's `server_listen_address` port through your firewall — from an outside machine, a TCP connect to it must succeed.
3. Confirm the validator is bonded: unbonded validators are omitted from `query valaddr providers`, so clients never see them.

### Clients report TLS identity verification failures

Clients verify that the server's certificate is endorsed by the consensus key of the validator they picked from the registry. A mismatch means the privval endpoint the server signs through does not hold the consensus key the chain knows for your validator — typical after pointing `--signer-grpc-address` at the wrong node (e.g. a sentry with its own key). The certificate is minted once at startup, so restart the server after any signer change.

### Uploads rejected with `payment promise verification failed`

The server validates every upload's payment promise against the app node. A chain-ID mismatch means `--app-grpc-address` points at a different network than the client used. Other causes sit on the submitter's side — an underfunded escrow account or a stale promise height — and resolve there, not on the server.

## Signals

- First `SIGINT`/`SIGTERM`: graceful shutdown
- Second signal: force shutdown
