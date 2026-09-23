# fibre-txsim

A load-generation tool that submits blobs to a Celestia network through the Fibre protocol. It connects to a validator's gRPC endpoint, creates random blobs, and sends them via `MsgPayForFibre` as fast as possible (or at a configured interval).

Payloads and namespaces use a worker-local ChaCha8 generator seeded from the
host, process, run timestamp, and worker index. Payloads of at least 40 bytes
include the worker identity and a sequence number. This synthetic randomness
is only for benchmark data; signing keys still come from the keyring.

Each concurrent worker gets its own signing key and account (e.g. `fibre-0`, `fibre-1`, ...), eliminating sequence number conflicts when running with `--concurrency > 1`.

Submission errors trigger per-worker exponential backoff with jitter, starting at
250–500 ms and capped at 5–10 seconds. Successful submissions reset the backoff.
The configured `--interval` remains a minimum delay; background confirmation and
download errors do not delay uploads. Shutdown cancels pending waits.


This binary is built for Linux and deployed to validator nodes by `make build-talis-bins`. It is started remotely via the `talis fibre-txsim` command.

## Build

```sh
# Cross-compile for talis VMs (Linux amd64)
make build-talis-bins

# Build for your local machine (useful for local testing)
go build -o fibre-txsim ./tools/fibre-txsim/
```

## Usage

```sh
fibre-txsim \
  --grpc-endpoint localhost:9091 \
  --keyring-dir .celestia-app \
  --key-prefix fibre \
  --blob-size 1000000 \
  --concurrency 4 \
  --interval 0s
```

## Flags

| Flag              | Default          | Description                                                                 |
|-------------------|------------------|-----------------------------------------------------------------------------|
| `--chain-id`      | *(optional)*     | Chain ID of the network (accepted for compatibility, unused)                |
| `--grpc-endpoint` | `localhost:9091` | gRPC endpoint of the validator                                              |
| `--keyring-dir`   | `.celestia-app`  | Path to the keyring directory                                               |
| `--key-prefix`    | `fibre`          | Key name prefix (keys are named `<prefix>-0`, `<prefix>-1`, ...)           |
| `--blob-size`     | `1000000`        | Size of each blob in bytes                                                  |
| `--concurrency`   | `1`              | Number of concurrent workers (each gets its own account)                    |
| `--interval`      | `0`              | Delay between blob submissions per worker (`0` = no delay)                  |
| `--duration`      | `0`              | How long to run (`0` = until killed with Ctrl+C)                            |
| `--upload-only`   | `false`          | Upload shards without broadcasting or confirming a transaction             |
| `--preencode`     | `false`          | Reuse one encoded blob with fresh payment promises |

With `--preencode`, each process encodes one random blob before the timed load
window. Every upload uses a fresh namespace and signed payment promise, creating
a new stored shard object. By default, uploads are followed by PFF broadcast and
confirmation; add `--upload-only` to measure ingestion without PFFs. Both modes
exclude random payload generation and encoding from the timed window; report
these results separately from throughput that includes fresh encoding.
It does not support `--download`. `--blob-size` specifies payload bytes, excluding
the blob header.

## How it works

1. Connects to a validator via gRPC and initializes a shared Fibre client.
2. Creates one worker per `--concurrency` slot, each with its own signing key (`fibre-0`, `fibre-1`, ...) and `TxClient`.
3. Each worker independently:
   - Generates a random namespace and random blob data of `--blob-size` bytes.
   - Encodes with `NewBlob`, calls `Upload`, and broadcasts `MsgPayForFibre` using its own key.
   - Hands off confirmation to background workers and logs upload and confirmation separately.
4. On shutdown (Ctrl+C or `--duration` elapsed), prints a summary with total sent, successes, failures, and average latency.

## Typical deployment

You don't normally run `fibre-txsim` directly. Instead, use `talis fibre-txsim` which SSHes into validators and starts it inside a tmux session:

```sh
talis fibre-txsim --directory <experiment-dir> \
  --instances 4 \
  --concurrency 2 \
  --blob-size 500000 \
  --duration 10m
```

See `tools/talis/fibre.md` for the full experiment workflow.

## Simulator metrics

Enable `--otel-endpoint http://collector:4318`. The service name is `fibre-txsim`;
its instance ID is the hostname. The `fibre.txsim` instruments describe simulator
work, independently of Fibre server traffic. The companion Grafana dashboard is
`observability/docker/grafana/dashboards/fibre-txsim.json`.

- `fibre.txsim.blobs`, `raw_bytes`, and `padded_bytes` count stage outcomes.
  `raw_bytes` excludes the header and padding; `padded_bytes` is `Blob.UploadSize()`
  (billed input, not Reed–Solomon expansion or retransmitted network bytes).
  Labels are `stage` and `outcome`; **never sum across stages**.
- `stage="upload",outcome="success"` means the SDK returned a quorum receipt,
  not that every provider's background upload finished. Broadcast success means
  broadcast returned successfully, not settlement. Only
  `stage="confirmation",outcome="success"` means observed successful execution.
- Generation, encoding, upload and broadcast failures/cancellation are explicit.
  Confirmation `failed` means rejection or nonzero execution code; `timeout` and
  `unknown` do **not** mean unpaid. A timed-out/evicted transaction may settle later.
  This simulator does not reconcile those outcomes later.
- Confirmation queue overflow is `untracked`, with blob and byte counts. The
  bounded queue still never blocks uploads; these broadcasts may settle but are
  absent from confirmed totals. Confirmed rates are therefore lower bounds when
  unknown, timeout or untracked counts are nonzero.
- `fibre.txsim.confirmation.pending` includes queued and polling requests.
  `confirmation.duration` measures encoding start through the observed outcome,
  including queue wait; its outcome label separates success from timeout/failure.
- `fibre.txsim.encoding.active` measures actual `NewBlob` calls. It is not an
  admission limiter. `pool.bytes` reports shared SDK capacity by pool and state:
  `in_use`, `free`, and `mapped`. **Mapped overlaps in-use/free; do not add them.**
  Capacity is not RSS. Linux `process.rss` includes resident mmap pages; platforms
  without `/proc/self/statm` omit this measurement. Existing Go runtime metrics
  remain available and do not account for all off-heap codec storage.

OTLP-to-Prometheus normalization produces `fibre_txsim_blobs_total`,
`fibre_txsim_raw_bytes_total`, `fibre_txsim_padded_bytes_total`,
`fibre_txsim_confirmation_pending`, `fibre_txsim_encoding_active`,
`fibre_txsim_pool_bytes`, `fibre_txsim_process_rss_bytes`, and
`fibre_txsim_confirmation_duration_seconds_{bucket,sum,count}` with the standard
Prometheus exporter naming strategy. Collector configuration can alter labels.
