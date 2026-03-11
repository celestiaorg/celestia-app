# fibre-txsim

A load-generation tool that submits blobs to a Celestia network through the Fibre protocol. It connects to a validator's gRPC endpoint, creates random blobs, and sends them via `MsgPayForFibre` as fast as possible (or at a configured interval).

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
  --chain-id <chain-id> \
  --grpc-endpoint localhost:9091 \
  --keyring-dir .celestia-app \
  --key-name validator \
  --blob-size 1000000 \
  --concurrency 4 \
  --interval 0s
```

## Flags

| Flag              | Default          | Description                                                                 |
|-------------------|------------------|-----------------------------------------------------------------------------|
| `--chain-id`      | *(required)*     | Chain ID of the network                                                     |
| `--grpc-endpoint` | `localhost:9091` | gRPC endpoint of the validator                                              |
| `--keyring-dir`   | `.celestia-app`  | Path to the keyring directory                                               |
| `--key-name`      | `validator`      | Key name in the keyring                                                     |
| `--blob-size`     | `1000000`        | Size of each blob in bytes                                                  |
| `--concurrency`   | `1`              | Number of concurrent blob submissions                                       |
| `--interval`      | `0`              | Delay between blob submissions (`0` = no delay, submit as fast as possible) |
| `--duration`      | `0`              | How long to run (`0` = until killed with Ctrl+C)                            |

## How it works

1. Connects to a validator via gRPC and initializes a Fibre client.
2. Spawns up to `--concurrency` goroutines that each:
   - Generate a random namespace and random blob data of `--blob-size` bytes.
   - Call `fibreClient.Put()` to submit the blob through the Fibre protocol.
   - Log the resulting block height, tx hash, and submission latency.
3. On shutdown (Ctrl+C or `--duration` elapsed), prints a summary with total sent, successes, failures, and average latency.

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
