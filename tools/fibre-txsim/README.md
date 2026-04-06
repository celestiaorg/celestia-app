# fibre-txsim

A load-generation tool that submits blobs to a Celestia network through the Fibre protocol. It connects to a validator's gRPC endpoint, creates random blobs, and sends them via `MsgPayForFibre` as fast as possible (or at a configured interval).

Each concurrent worker gets its own signing key and account (e.g. `fibre-0`, `fibre-1`, ...), eliminating sequence number conflicts when running with `--concurrency > 1`.

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

## How it works

1. Connects to a validator via gRPC and initializes a shared Fibre client.
2. Creates one worker per `--concurrency` slot, each with its own signing key (`fibre-0`, `fibre-1`, ...) and `TxClient`.
3. Each worker independently:
   - Generates a random namespace and random blob data of `--blob-size` bytes.
   - Calls `fibre.PutWithKey()` to submit the blob through the Fibre protocol using its own key.
   - Logs the resulting block height, tx hash, and submission latency.
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
