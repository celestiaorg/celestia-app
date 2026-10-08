# Fibre Server

Standalone binary for the Fibre data availability server.

## Prerequisites

Before starting, make sure:

- [ ] A `celestia-appd` node is available. The app link (`--app-grpc-address`) is not TLS-protected. The signer link (`--signer-grpc-address`) allows plaintext only on loopback and requires mutual TLS for a remote node — see [Signing](#signing).
- [ ] The chain is on **app version 10 or later**. The `x/fibre` and `x/valaddr` modules the server depends on do not exist in earlier versions.
- [ ] The node's application gRPC endpoint is enabled in `app.toml` — see [Node connections](#node-connections).
- [ ] The node's privval gRPC endpoint is enabled — see [Signing](#signing). If the consensus key lives in an external KMS, the KMS must support the privval `SignRawBytes` message; see the [release notes](../../docs/release-notes/release-notes.md) for the KMS policy.
- [ ] The fibre listen port (default `7980`) is reachable by clients from outside your network.
- [ ] Your validator is bonded. The server derives its storage budget from your stake; a validator outside the active set gets no budget and no traffic.

We recommend storing fibre server data and celestia-app data on separate disks. This prevents unexpected storage growth in either service from consuming the disk space available to the other. Use `--home` or `FIBRE_HOME` to place the fibre home directory on a separate disk (see [Start](#start)).

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

### Node connections

Fibre uses two connections to your validator's node. The application gRPC service and the core RPC gRPC service are separate listeners:

| Service | Node configuration | Example address | Fibre flag |
|---|---|---|---|
| Application gRPC | `config/app.toml`, `[grpc] address` | `127.0.0.1:9090` | `--app-grpc-address` |
| Core RPC gRPC | `config/config.toml`, `[rpc] grpc_laddr` | `tcp://127.0.0.1:9098` | None |
| Privval gRPC | `config/config.toml`, top-level `priv_validator_grpc_laddr` | `127.0.0.1:26669` | `--signer-grpc-address` |

Paths are relative to your node's home directory. For a same-host deployment, enable application gRPC by editing the existing `[grpc]` section in `config/app.toml`:

```toml
[grpc]
enable = true
address = "127.0.0.1:9090"
```

For separate hosts, bind application gRPC to the validator's private IP instead, for example `address = "10.0.0.5:9090"`, and set Fibre's `--app-grpc-address 10.0.0.5:9090` to match. Replace the example IP with your validator's private address and restrict access to the Fibre host with a firewall. Keep this plaintext connection on a trusted private network or encrypted tunnel. `127.0.0.1` on the Fibre host refers to that machine, so it cannot reach the validator's loopback listener directly.

Application gRPC is disabled in a freshly generated app config. Enable it explicitly so it remains available when the multiplexer switches to v10. Restart the node after changing its configuration; also check for service-manager flags that override these values.

Keep the existing `[rpc] grpc_laddr` address and port in `config/config.toml` so existing core RPC clients, including bridge nodes, can continue using it. Moving that listener from `9098` to `9090` does not make it an application gRPC service and can cause a port conflict. Custom ports work: point Fibre at the actual application and privval listeners.

Use `host:port` without `tcp://` for application gRPC, privval gRPC, and Fibre's connection flags. The core RPC listener uses `tcp://host:port`. Keep Fibre's application and signer connections on loopback or a trusted private network; only its client listen port (default `7980`) needs to be publicly reachable.

### Start

Start Fibre after the chain activates app version 10. Installing a v10 binary alone does not activate v10. Check your node's running app version through its HTTP RPC endpoint:

```sh
curl -s http://127.0.0.1:26657/abci_info
```

Confirm `result.response.app_version` is `10` or later before starting:

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

Override config values with flags (flags take precedence over config file). This example uses same-host connections:

```sh
fibre start \
  --app-grpc-address 127.0.0.1:9090 \
  --server-listen-address 0.0.0.0:7980 \
  --signer-grpc-address 127.0.0.1:26669
```

For separate hosts, replace both loopback addresses with the validator's private addresses and configure signer mutual TLS as described in [Node connections](#node-connections) and [Signing](#signing).

### Version

```sh
fibre version
```

## Config

The config file is at `$FIBRE_HOME/config/server_config.toml` (default `~/.celestia-fibre/config/server_config.toml`).

After upgrading, add missing settings and their documentation with `celestia-appd config sync --fibre-home ~/.celestia-fibre`. It preserves existing values and backs up the file first; add `--dry-run` to preview.

`min_upload_size` sets this validator's minimum padded Fibre upload size in bytes, including the header and excluding parity. It defaults to `262144` (256 KiB), including when the field is absent from an existing config. To require 32 MiB uploads, set:

```toml
min_upload_size = 33554432
```

Restart Fibre to apply changes. Values must be positive and no greater than the 128 MiB protocol maximum. This is a local admission policy: it does not change encoding, block validation, settlement, or downloads of existing blobs. Clients are not automatically informed of the setting and smaller uploads are rejected rather than padded by the server. Coordinate settings across validators and clients; incompatible minimums can prevent clients from collecting enough signatures.

Config precedence: **flag > config file > default**. New fields added in a release do not appear in an existing config file automatically; add them by hand to override their default. Changes take effect on restart.
Unknown keys and tables cause startup to fail, so Fibre does not silently ignore misspelled settings.

### Object storage credentials

Fibre needs credentials to reach the bucket configured in `[object_storage]`. It reads them from its environment using the standard AWS variables and refuses to start if none are found. There is no credentials field in `server_config.toml`.

1. Create an access key in your storage provider with read, write, and delete permissions on the bucket:

   - **Amazon S3**: create an IAM user with `s3:GetObject`, `s3:PutObject`, `s3:DeleteObject`, and `s3:ListBucket` on the bucket, then create an access key for it.
   - **Cloudflare R2**: create an R2 API token with **Object Read & Write** permission scoped to the bucket. Use its Access Key ID and Secret Access Key below.

2. Save the key in `/etc/fibre/s3.env`, readable only by root:

   ```sh
   sudo mkdir -p /etc/fibre
   sudo tee /etc/fibre/s3.env > /dev/null <<'EOT'
   AWS_ACCESS_KEY_ID=replace-with-your-access-key-id
   AWS_SECRET_ACCESS_KEY=replace-with-your-secret-access-key
   EOT
   sudo chmod 0600 /etc/fibre/s3.env
   ```

   If AWS gave you temporary credentials, add a third line with `AWS_SESSION_TOKEN=...`.

3. Point the Fibre systemd service at the file. Run `sudo systemctl edit fibre.service` (use your service name if it differs) and add:

   ```ini
   [Service]
   EnvironmentFile=/etc/fibre/s3.env
   ```

4. Restart Fibre and check that it started:

   ```sh
   sudo systemctl daemon-reload
   sudo systemctl restart fibre.service
   sudo systemctl status fibre.service
   ```

   If Fibre exits with `loading object storage credentials`, the service did not receive the key. Check the file and the service override, then restart. A key with wrong permissions shows up later as access denied errors in the logs.

To rotate keys, update `/etc/fibre/s3.env` and restart Fibre. If the host already has credentials from an IAM role or the shared AWS credentials file, Fibre uses them, and you can skip this section.

### Switching shard storage backends

Changing `storage_backend` between `local` and `object` only changes where new shards are stored. Existing shards stay on their original backend.

**Warning:** After switching back to `local`, keep `[object_storage]` configured until all object shards are pruned. Keep access to the same bucket and prefix, with valid credentials, throughout the retention window. While these shards remain, do not remove the object data or change the configured bucket or prefix. To move them, follow [Migrating to another bucket or prefix](#migrating-to-another-bucket-or-prefix).

Local mode still uses object storage to read and prune retained object shards. Missing object storage configuration or credentials prevents startup.

### Migrating to another bucket or prefix

At startup, Fibre records the object endpoint, bucket, prefix, chain ID, and validator address it uses. While object shards remain, starting with a different endpoint, bucket, or prefix fails in either storage mode. This stops retained shards from becoming unreachable by accident; if it happens, restore the previous configuration to recover access. To move them on purpose:

1. Stop Fibre, so no new shards are written to the old bucket during the copy.
2. Create the new bucket in the same region as the Fibre host. A bucket in another region adds latency to every upload and download.
3. Copy every object under the old prefix to the new bucket with the same key layout, for example `aws s3 sync s3://<old-bucket>/<prefix> s3://<new-bucket>/<prefix>`. Fibre looks shards up by key, so a different layout makes them unavailable.
4. Set the new endpoint, bucket, or prefix in `[object_storage]`. If the new bucket uses different keys, update the [credentials](#object-storage-credentials) too.
5. Start Fibre once with `--override-object-namespace`. It logs the old and new namespaces and records the new one. The flag does not copy or verify objects, so an override before the copy is complete makes shards unavailable and leaves orphaned objects after pruning.
6. Remove the flag and restart Fibre. The flag is not saved in the config file, and leaving it in the start command would accept any future mismatch without protection.
7. Once Fibre serves from the new bucket, delete the old bucket and revoke its credentials.

Startup fails if the saved record is corrupt, or if retained object shards have no namespace record. After all object shards are pruned, namespace changes need no override.

### RPC memory admission

Fibre admits uploads and downloads against a shared working-memory budget:

```toml
rpc_memory_budget = 10737418240 # 10 GiB total
```

Downloads can fill the budget. Uploads cannot increase total usage above 75%.
Requests that exceed these limits fail immediately with `ResourceExhausted`. Admission reserves memory before upload receipt or stored-shard reads.
Uploads use the message size; downloads use stored size markers, with a 14% stake sizing fallback for missing metadata.
Reservations remain held until the handler and gRPC response buffers finish.

Requests must arrive within 15 seconds; this receive timeout excludes verification and storage.
These are memory estimates, not bandwidth guarantees or a process-memory ceiling. Leave headroom for transport buffers, GC and persistent state.
The gauges `fibre.server.rpc.reserved_bytes` and `fibre.server.rpc.upload_reserved_bytes` show total and upload reservations.
The counter `fibre.server.rpc.memory_rejected` records budget rejections.

#### Bypassing admission

Set `rpc_memory_budget = 0` and restart Fibre. Protocol limits and the receive timeout still apply.

## Signing

Fibre signs payment promises by connecting to the consensus node's `PrivValidatorAPI` gRPC endpoint. The node handles its own key management (local key, tmkms, etc.) — fibre just delegates signing to it.

Fresh v10 `celestia-appd init` configurations enable the privval gRPC endpoint on `127.0.0.1:26669`. Existing configurations keep their saved value, which may be `127.0.0.1:26659`, a custom address, or empty (disabled). Replacing the binary does not rewrite that value. The new default avoids a port clash with TMKMS.

With celestia-app v10.2.0 or later, sync the node's configuration first to add missing fields and their documentation:

```sh
celestia-appd config sync --home ~/.celestia-app
```

Use your node's home directory if it differs. The command preserves existing values, including an empty or old signer address. Then enable or change the top-level setting in `config/config.toml`, before any section such as `[rpc]`:

```toml
priv_validator_grpc_laddr = "127.0.0.1:26669"
```

If you change the port, also update Fibre's `signer_grpc_address` in `server_config.toml` or its `--signer-grpc-address` flag, then restart the node and Fibre. A working custom port can be retained if it does not conflict with another listener and Fibre uses the same address. Update deployment-managed config templates too.

This default loopback connection uses plaintext. To run Fibre on a separate
host, use a dedicated certificate authority to issue a server certificate for
the node and a client certificate for Fibre. The server certificate's subject
alternative name must match the address Fibre uses to reach the node.

Configure the node's `config.toml`:

```toml
priv_validator_grpc_laddr = "10.0.0.5:26669"
priv_validator_grpc_cert_file = "/etc/celestia/privval/server.crt"
priv_validator_grpc_key_file = "/etc/celestia/privval/server.key"
priv_validator_grpc_client_ca_file = "/etc/celestia/privval/ca.crt"
```

Then configure Fibre's `server_config.toml` with the same CA and its client
certificate:

```toml
signer_grpc_address = "10.0.0.5:26669"
signer_grpc_ca_file = "/etc/celestia/privval/ca.crt"
signer_grpc_cert_file = "/etc/celestia/privval/client.crt"
signer_grpc_key_file = "/etc/celestia/privval/client.key"
```

All three TLS files must be set together on each side. Restart the node and
Fibre after changing them. Fibre rejects a non-loopback plaintext signer unless
`signer_grpc_allow_insecure` is explicitly enabled; this override is not
recommended because anyone with network access to the endpoint can request
signatures.

**Fibre always connects to the node, never to the KMS directly, so the fibre config is the same for every key backend.**

Whatever the backend, median signing latency must stay at or below 10ms. Nodes using a remote signer expose `cometbft_privval_signing_latency_*` metrics and log a warning when the median of the last 50 signatures exceeds it.

## Registration

Fibre clients discover servers through the on-chain [`x/valaddr`](../../x/valaddr/README.md) registry and only dial hosts registered there. Once the server is up, register its publicly reachable address, signing with your validator's account key:

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

The Fibre server↔client link is always TLS-encrypted, and it is fully automatic: there are no certificates to obtain, configure, or renew.

- On startup the server generates its own certificate and has it endorsed once by your validator's consensus key (through the signer). Clients verify that endorsement against the validator set, so the connection proves it belongs to your validator — no certificate authority involved.
- Identity is bound to the consensus key, not the network address, so the host you register on-chain can be an IP literal or a DNS name.
- A restart generates a fresh certificate automatically. After changing the signer (`--signer-grpc-address`), restart the server so the certificate is endorsed with the right key.
- Downloads are public — any peer can read shards. Uploads are still gated by the payment-promise check.

Two things to keep in mind:

- The app link (`--app-grpc-address`) is not TLS-protected, so keep it on the same host or a trusted network. The signer link (`--signer-grpc-address`) uses plaintext only on loopback; remote connections require the mutual TLS configuration described in [Signing](#signing).
- There is no plaintext fallback, so every Fibre server and client on the network must run a TLS-capable build.

For the full design (endorsement scheme, certificate format, OIDs), see the [Fibre server spec](../../specs/src/fibre_server.md).

## Troubleshooting connections

- **`unknown service cosmos.base.tendermint.v1beta1.Service`**: Fibre could not query node information from its application connection. Check `--app-grpc-address`, the `[grpc]` section of `app.toml`, and the chain's active app version. The core RPC listener is not a substitute for application gRPC. This error alone does not establish that the only problem is pending v10 activation.
- **Missing Fibre or valaddr services before activation**: prepare the configuration now, but start Fibre and register its host only once app version 10 is active.
- **Application connection refused**: enable application gRPC, restart the node, and check that Fibre uses the same address and port. Flags passed by the service manager can override the file.
- **Signer connection failed**: compare Fibre's signer address with the node's top-level `priv_validator_grpc_laddr`. Check for an empty value, the old port, or a listener conflict.

For new node settings and their comments, see [Updating existing configuration files](../../docs/release-notes/release-notes.md#updating-existing-configuration-files). `update-config` has no v10 migration.

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

Fibre exports traces and metrics via OTLP/HTTP to an OpenTelemetry collector (Grafana Alloy, OTel Collector, etc.). Both signals share the same base URL and are enabled together. Configure the collector to forward both metrics and traces to their backends.

| Flag | Env | Default |
|---|---|---|
| `--otel-endpoint` | `FIBRE_OTEL_ENDPOINT` | *(disabled)* |

```sh
fibre start --otel-endpoint http://localhost:4318
```

Fibre appends `/v1/traces` and `/v1/metrics` to the base URL. A proxy prefix is preserved: `https://collector.example.com/otel` sends traces to `/otel/v1/traces` and metrics to `/otel/v1/metrics`.

Set `--otel-endpoint` or `FIBRE_OTEL_ENDPOINT` to the base URL only. If your existing configuration ends in `/v1/metrics` or `/v1/traces`, remove that suffix when upgrading.

**Tracing** — The sampler uses `ParentBased(TraceIDRatioBased(0.1))` — 10% of root spans are sampled, and sampling decisions from upstream services are respected.

W3C TraceContext and Baggage propagators are registered globally, enabling distributed trace context to flow across gRPC and HTTP boundaries.

Resource attributes exported with every trace: `service.name=fibre`, `service.version`, `service.instance.id` (hostname).

**Metrics** — Exported via a periodic OTLP reader. Duration histograms carry a `success` or `outcome` attribute for error rate derivation from `_count`. Size attributes (`blob_size`, `upload_size`, `shard_size`) are rounded up to the next power of two to bound cardinality. Exemplars are automatically attached to metric observations, linking metric datapoints to traces — in Grafana, clicking an exemplar on a metric panel opens the corresponding trace.

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
| `fibre.server.upload_shard.bytes` | Counter (By) | — | Total shard row bytes stored |
| `fibre.server.upload_shard.rejected` | Counter | `reason` | UploadShard RPCs rejected by the storage limiter |
| `fibre.server.upload_shard.dupe_hits` | Counter | `stage` | UploadShard RPCs for an already stored shard |
| `fibre.server.upload_shard.occupancy_bytes` | Gauge (By) | — | Shard bytes tracked by the storage limiter (on-disk plus reserved) |
| `fibre.server.upload_shard.budget_bytes` | Gauge (By) | — | Current per-node storage budget |
| `fibre.server.upload_shard.last_success_timestamp` | Gauge (s) | — | Unix time of the last successful UploadShard RPC; not reported before the first one |
| `fibre.server.download_shard.in_flight` | UpDownCounter | — | Concurrent DownloadShard RPCs |
| `fibre.server.download_shard.duration` | Histogram (s) | `success`, `shard_size` | DownloadShard RPC latency |
| `fibre.server.download_shard.bytes` | Counter (By) | — | Total shard row bytes served |
| `fibre.server.store.put.duration` | Histogram (s) | `success` | Store write latency |
| `fibre.server.store.get.duration` | Histogram (s) | `success` | Store read latency |
| `fibre.server.backend.get.duration` | Histogram (s) | `backend`, `outcome` | Backend GET latency through payload reading, decoding and closing |
| `fibre.server.backend.get.in_flight` | UpDownCounter | `backend` | Concurrent backend GET calls |
| `fibre.server.backend.get.bytes` | Counter (By) | `backend` | Encoded bytes consumed, including partial failures and buffered reads |
| `fibre.server.sign.duration` | Histogram (s) | `success` | Payment promise signing latency |
| `fibre.server.prune.entries` | Counter | — | Total entries pruned |
| `fibre.server.prune.duration` | Histogram (s) | `success` | Prune cycle duration |

Backend GET metrics record only the primary backend and use `backend=local|object`.
The duration metric uses `outcome=success|not_found|timeout|canceled|throttled|error`.
Each observation covers one backend call. Object GET duration includes SDK retries; the outcome describes the final result.

#### Grafana dashboard

A pre-built Grafana dashboard is available at [`observability/docker/grafana/dashboards/fibre.json`](../../observability/docker/grafana/dashboards/fibre.json).

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
