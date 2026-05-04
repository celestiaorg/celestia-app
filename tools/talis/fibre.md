# Running Fibre Experiments with Talis

This guide covers running Fibre throughput experiments. For general talis setup (prerequisites, installation, cloud provider config, spinning up nodes, and tearing them down), see the main [README.md](README.md).

## Overview

A fibre experiment has up to five phases:

1. **Setup** — Register fibre host addresses and fund escrow accounts on each validator.
2. **Start fibre server** — Start the fibre server on each validator.
3. **Load generation** — Start `fibre-txsim` on one or more validators or encoders to submit blobs via the Fibre protocol.
4. **Read load** *(optional)* — Start `fibre-reader` on dedicated reader instances to subscribe to the chain, scan `MsgPayForFibre`, and download the referenced blobs. Lets you measure aggregate per-cluster *read* throughput end-to-end.
5. **Monitoring** — Run `fibre-throughput` to observe per-block throughput in real time and optionally write structured traces to a JSONL file.

## Prerequisites

Follow the main [README.md](README.md) through the **deploy** step so you have a running network:

```sh
talis init --chain-id <chain-id> --experiment <experiment>
talis add --type validator --count <count>
talis up
talis genesis --square-size 256 --build-dir build
talis deploy --direct-payload-upload --workers 20
```

## 1. Fibre setup

Register each validator's fibre host address and deposit tokens into escrow for all fibre worker accounts:

```sh
talis setup-fibre
```

| Flag               | Default               | Description                                          |
|--------------------|-----------------------|------------------------------------------------------|
| `--directory`      | `.`                   | Experiment root directory                            |
| `--ssh-key-path`   | *(from env/config)*   | Path to SSH private key                              |
| `--escrow-amount`  | `200000000000000utia` | Amount to deposit into escrow per account            |
| `--fibre-port`     | `7980`                | Fibre gRPC port on validators                        |
| `--fees`           | `5000utia`            | Transaction fees                                     |
| `--workers`        | `10`                  | Number of validators to set up in parallel           |
| `--fibre-accounts` | `100`                 | Number of fibre worker accounts to deposit escrow for|

This SSHes into every validator and runs the `set-host` and `deposit-to-escrow` transactions (one per fibre account). It polls tmux sessions to wait for all transactions to complete before returning.

## 2. Start fibre server

Start the fibre server on validators:

```sh
talis start-fibre
```

| Flag                | Default             | Description                                                   |
|---------------------|---------------------|---------------------------------------------------------------|
| `--directory`       | `.`                 | Experiment root directory                                     |
| `--ssh-key-path`    | *(from env/config)* | Path to SSH private key                                       |
| `--instances`       | `0` (all)           | Number of validators to start fibre on                        |
| `--otel-endpoint`   | *(auto)*            | OTLP HTTP endpoint for metrics/traces (auto-enabled with observability) |

The fibre server delegates signing to the colocated validator node's PrivValidatorAPI gRPC endpoint (default `127.0.0.1:26659`). Override with `--signer-grpc-address` if needed. Metrics and traces are auto-enabled via OTLP when observability nodes are configured.

Each validator runs the fibre server inside a tmux session called `fibre`. To stop:

```sh
talis kill-session --session fibre
```

## 3. Start fibre-txsim

Start blob submission on one or more validators:

```sh
talis fibre-txsim --instances 4 \
  --concurrency 2 \
  --blob-size 1000000
```

| Flag             | Default             | Description                                                              |
|------------------|---------------------|--------------------------------------------------------------------------|
| `--directory`    | `.`                 | Experiment root directory                                                |
| `--ssh-key-path` | *(from env/config)* | Path to SSH private key                                                  |
| `--instances`    | `1`                 | Number of validators to start fibre-txsim on                             |
| `--concurrency`  | `1`                 | Concurrent blob submissions per instance (each gets its own account)     |
| `--blob-size`    | `1000000`           | Size of each blob in bytes                                               |
| `--interval`     | `0`                 | Delay between submissions per worker (`0` = no delay)                    |
| `--duration`     | `0`                 | How long to run (`0` = until killed)                                     |
| `--key-prefix`   | `fibre`             | Key name prefix in keyring (keys are named `<prefix>-0`, `<prefix>-1`, ...) |

Each concurrent worker gets its own signing key and account (e.g. `fibre-0`, `fibre-1`, ...), eliminating sequence number conflicts.

Each instance runs inside a tmux session called `fibre-txsim` on the remote validator. To stop all instances:

```sh
talis kill-session --session fibre-txsim
```

To view logs on a specific validator:

```sh
ssh root@<ip> 'cat /root/talis-fibre-txsim.log'
```

## 4. Start fibre-reader (optional)

Read-side load: subscribes each reader instance to a pinned validator's RPC, scans `MsgPayForFibre` from every new block, and concurrently downloads owned blobs via `fibre.Client`. Use it to measure how fast a cluster can *read* what `fibre-txsim` produces.

### Prerequisite: add reader instances

Reader instances need to be added to the experiment config and deployed alongside validators/encoders. Do this *before* `talis up`:

```sh
talis add --type reader --provider aws --count 5
talis up
talis genesis --ods-size 256 --build-dir build
talis deploy --workers 20
```

`talis genesis` stages a `reader-payload/` directory (fibre-reader binary + a fibre keyring borrowed from validator-0). `talis deploy` ships it to each reader and runs `reader_init.sh`, which installs `/bin/fibre-reader` and `/root/.celestia-app/keyring-test/`. Mirrors the encoder pattern.

The fibre-reader binary must be built with `-tags=ledger,fibre` so its codec can decode `MsgPayForFibre`. `make build-talis-bins-fibre` does this automatically.

`fibre-txsim` needs to be running with `--upload-only=false` so PFF transactions actually land on chain — otherwise readers see nothing to download.

### Run

```sh
talis fibre-reader \
  --download-concurrency 8 \
  --download-timeout 2m
```

| Flag                     | Default | Description                                                                                                                  |
|--------------------------|---------|------------------------------------------------------------------------------------------------------------------------------|
| `--directory`            | `.`     | Experiment root directory                                                                                                    |
| `--instances`            | `0` (all) | Max number of readers to launch                                                                                            |
| `--download-concurrency` | `32`    | Max concurrent in-flight downloads per reader (semaphore-bounded; goroutine spawned per blob)                                |
| `--download-timeout`     | `2m`    | Per-blob download timeout                                                                                                    |
| `--duration`             | `0`     | How long to run (`0` = until killed)                                                                                         |
| `--key-prefix`           | `fibre` | Fibre keyring key-name prefix (only used to satisfy `fibre.NewClient`'s key-existence check; reader does not sign anything)  |
| `--pyroscope-endpoint`   | *(auto)* | Pyroscope endpoint (auto-detected from observability config)                                                                |

### Sharding

Each reader is launched with `--reader-index N --reader-count K` (talis fills these in automatically based on the number of configured readers). For each blob's commitment, the reader computes `binary.BigEndian.Uint64(commitment[:8]) % count == index` to decide ownership. Commitments are SHA-derived so the distribution is uniform — every blob is downloaded exactly once across the cluster, no coordinator needed.

### Validator pinning

`reader[i]` round-robin pins to `validator[i % len(validators)]` for both the cometbft RPC subscription (`:26657`) and the fibre gRPC state endpoint (`:9091`). This distributes chain-watch load across validators; the actual download still fans out across all validators inside `fibre.Client.Download`.

### Memory notes

`--download-concurrency 32` (default) can OOM on hosts with `<= 64 GiB` RAM when running with 128 MiB blobs — each in-flight download holds an extended (parity-doubled) blob buffer plus per-validator gRPC scatter buffers, easily 1+ GiB per slot at the high end. On `c6in.8xlarge` (64 GiB), `--download-concurrency 8` is the safe upper bound until the Reed-Solomon coder is pooled (see Pyroscope `inuse_space` profile dominated by `reedsolomon.AllocAligned`).

### Logs and lifecycle

Each reader runs inside a tmux session named `fibre-reader`. To stop:

```sh
talis kill-session --session fibre-reader
```

To tail logs:

```sh
ssh root@<reader-ip> 'tail -f /root/talis-fibre-reader.log'
```

### Metrics

Per-reader OTel metrics (auto-pushed to the observability node when configured), labeled by `service_instance_id`:

| Metric                                               | Type         | Use                                                                                          |
|------------------------------------------------------|--------------|----------------------------------------------------------------------------------------------|
| `fibre_reader.blobs_seen`                            | Counter      | Total `MsgPayForFibre` observed in blocks                                                    |
| `fibre_reader.blobs_owned`                           | Counter      | Blobs assigned to this reader by sharding                                                    |
| `fibre_reader.blobs_skipped_not_owned`               | Counter      | Blobs that hashed to a different reader's shard                                              |
| `fibre_reader.downloads_success` / `_failed`         | Counter      | Outcomes                                                                                     |
| `fibre_reader.commitment_mismatches`                 | Counter      | Downloads that returned `ErrBlobCommitmentMismatch`                                          |
| `fibre_reader.downloaded_bytes_total` *(`By`)*       | Counter      | `rate(...) by (service_instance_id) / 1024 / 1024` → per-reader MiB/s in Grafana             |
| `fibre_reader.download_latency_ms`                   | Histogram    | Time spent in `fibre.Client.Download` per blob                                               |
| `fibre_reader.queue_wait_ms`                         | Histogram    | Time a download goroutine spent waiting for a semaphore slot — non-zero p50 = sem-bound      |
| `fibre_reader.e2e_latency_ms`                        | Histogram    | `now − PaymentPromise.CreationTimestamp` at download success                                 |
| `fibre_reader.inclusion_to_download_latency_ms`      | Histogram    | `now − block.Time` at download success                                                       |
| `fibre_reader.block_processing_latency_ms`           | Histogram    | Decode + scan + dispatch per block                                                           |

OTel tracing spans `fibre_reader.block.process` and `fibre_reader.blob.download` (with the latter wrapping `fibre.Client.Download` and its child `download_from` per-validator spans). Use these to debug per-step timing.

## 5. Monitor throughput

Run `fibre-throughput` from your local machine to poll blocks and print per-block stats:

```sh
talis fibre-throughput
```

This connects to the first validator's RPC endpoint and prints a line per block:

```text
height=350 pff_txs=4 pfb_txs=0 pff_bytes=3MB pfb_bytes=0MB block_time=3.06s pff_throughput=1.02MB/s pfb_throughput=0.00MB/s
```

### Flags

| Flag             | Default                      | Description                                   |
|------------------|------------------------------|-----------------------------------------------|
| `--directory`    | `.`                          | Experiment root directory                     |
| `--rpc-endpoint` | *(first validator IP:26657)* | CometBFT RPC endpoint to poll                 |
| `--duration`     | `0`                          | How long to run (`0` = until Ctrl+C)          |
| `--start-height` | `0`                          | Block height to start from (`0` = latest + 1) |
| `--with-traces`  | `false`                      | Enable JSONL trace file output                |
| `--traces-dir`   | `traces/throughput`          | Directory where trace files are written       |

### Writing traces

To record structured per-block data for later analysis, enable the `--with-traces` flag:

```sh
talis fibre-throughput --directory <experiment-dir> --with-traces
```

This creates a timestamped JSONL file inside the traces directory:

```text
traces/throughput/throughput_2026-02-18T20:59:35Z.jsonl
```

Each run creates a new file. To use a custom directory:

```sh
talis fibre-throughput --directory <experiment-dir> --with-traces --traces-dir my/traces
```

Each line in the JSONL file is a JSON object with the following fields:

```json
{
  "height": 350,
  "timestamp": "2026-02-18T20:59:33Z",
  "block_time_sec": 3.06,
  "pff_count": 4,
  "pfb_count": 0,
  "total_pff_bytes": 4000000,
  "total_pfb_bytes": 0,
  "pff_throughput_mbs": 1.25,
  "pfb_throughput_mbs": 0
}
```

| Field                | Description                                                |
|----------------------|------------------------------------------------------------|
| `height`             | Block height                                               |
| `timestamp`          | Block header timestamp (RFC 3339)                          |
| `block_time_sec`     | Seconds since the previous block                           |
| `pff_count`          | Number of `MsgPayForFibre` transactions                    |
| `pfb_count`          | Number of `MsgPayForBlobs` transactions                    |
| `total_pff_bytes`    | Total PFF blob bytes in the block                          |
| `total_pfb_bytes`    | Total PFB blob bytes in the block                          |
| `pff_throughput_mbs` | PFF throughput in MB/s (`pff_bytes / block_time / 1024^2`) |
| `pfb_throughput_mbs` | PFB throughput in MB/s (`pfb_bytes / block_time / 1024^2`) |

### Replaying past blocks

To analyze blocks from a past experiment, use `--start-height`:

```sh
talis fibre-throughput --directory <experiment-dir> --with-traces --start-height 100
```

## 6. Teardown

When the experiment is complete:

```sh
# Stop fibre-reader (if started), fibre-txsim, and fibre server
talis kill-session --session fibre-reader
talis kill-session --session fibre-txsim
talis kill-session --session fibre

# Tear down cloud instances
talis down --workers 20
```
