# Corto-10 deployment example

A record of how a Corto-10 validator host is configured. These are the live
`c8gn.12xlarge` (arm64) values, not recommended celestia-app defaults. Homes are
`/var/lib/corto-10/app` (appd) and `/var/lib/corto-10/fibre` (fibre).

## Service units

- `corto-app.service` — validator. Runs with `GOMEMLIMIT=8GiB`, `GOMAXPROCS=8`,
  `MemoryMax=16G`.
- `corto-fibre.service` (+ `corto-fibre.service.d/60-telemetry.conf`) — fibre
  sidecar. Runs with `GOMEMLIMIT=48GiB`, `GOMAXPROCS=32`, `MemoryMax=64G`.

**Experiment-only flags (Corto-only, not recommended defaults):** the appd unit
passes `--timeout-commit=500ms`, `--delayed-precommit-timeout=1s` and
`--pff-proposal-limit=500`. The first two alter consensus timing; the last raises
the per-proposal PFF limit. They exist to push throughput on a bounded testnet.

## Config deltas (live vs code default)

`class`: (b) deliberate per-deployment, (c) experiment-only. `[C]` =
consensus-relevant. Full release defaults are in
`../corto-10-config-baselines-20260929/`.

### app.toml

| setting | live | default | class |
|---|---|---|---|
| `pff-proposal-limit` | 500 | 0 | (c) |
| `pruning` / `keep-recent` / `interval` | custom / 100000 / 10 | default / 0 / 0 | (b) |
| `min-retain-blocks` | 350000 | 3000 | (b) |
| `index-events` | `["message.action"]` | `[]` | (b) |
| `grpc.address` | `<private-ip>:9090` | `localhost:9090` | (b) |

### config.toml

| setting | live | default | class |
|---|---|---|---|
| `consensus.timeout_commit` `[C]` | 500ms | 1s | (c) |
| `consensus.delayed_precommit_timeout` `[C]` | 1s | 0 (disabled) | (c) |
| `p2p.pex` / `addr_book_strict` | false / false | true / true | (b) |
| `rpc.laddr` / TLS | private IP / LE cert | 127.0.0.1 / "" | (b) |
| `storage.compact` | true | false | (b) |
| `tx_index.indexer` | kv | null | (b) |
| `instrumentation.prometheus` / `trace_type` | true / noop | false / local | (b) |

### fibre server_config.toml

See `server_config.toml.example`.

| setting | live | default | class |
|---|---|---|---|
| `server_listen_address` / `app_grpc_address` | private-IP binds | `0.0.0.0:7980` / `127.0.0.1:9090` | (b) |
| `upload_verify_workers` | 32 | 100 | (b) |
| `max_connections` | 32 | 16 | (b) |
| `max_concurrent_streams` | 16 | 200 | (b) |
| `storage_backend` | object (S3) | local | (b) |

## Host tuning

sysctl drop-ins (`/etc/sysctl.d/`):

- `sysctl-90-corto-10.conf` — `vm.swappiness=1`, `fs.file-max=2097152`.
- `sysctl-91-corto-10-bbr.conf` — `net.core.default_qdisc=fq`,
  `net.ipv4.tcp_congestion_control=bbr`.
- `sysctl-92-corto-20x-tcp.conf` — larger `tcp_rmem`/`tcp_wmem`,
  `tcp_slow_start_after_idle=0`.

On top of the `fq` default qdisc, a oneshot unit sets `fq` on each leaf of the
primary NIC's multiqueue (`mq`) root, preserving the topology.

## Fibre memory

Worst-case fibre RAM is roughly:

```
max_connections × max_concurrent_streams × ~132 MiB
```

- Corto-10: `32 × 16 × 132 MiB` ≈ 66 GiB — sized by `MemoryMax=64G`.
- Code default: `16 × 200 × 132 MiB` ≈ 413 GiB — unsafe on a typical host.

Size `max_concurrent_streams` and `max_connections` so this product fits the
host's `MemoryMax`.
