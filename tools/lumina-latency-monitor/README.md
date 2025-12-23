# Lumina Latency Monitor

A Rust-based tool for monitoring and measuring transaction latency in Celestia networks using the Lumina gRPC client. This tool submits PayForBlob transactions at a specified rate and measures the time between submission and commitment, providing detailed latency statistics.

## Features

- Configurable submission delay between transactions
- Random blob sizes between configurable minimum and maximum bounds
- Custom namespace support
- Real-time transaction monitoring with split broadcast/confirm pattern
- Parallel confirmation tracking (up to 100 in-flight transactions)
- Tracks both successful and failed confirmations
- Detailed latency statistics including mean and standard deviation
- CSV export of all transaction results with failure tracking
- Keyring support compatible with celestia-app

## Prerequisites

- A running Celestia node with gRPC enabled or access to an existing endpoint
- Rust toolchain (cargo)
- Access to a keyring with funds for transaction submission (or a private key)

## Building

```bash
cargo build --release
```

## Usage

```bash
./lumina-latency-monitor [flags]
```

### Available Flags

| Flag                 | Shorthand | Default           | Description                                                                       |
| -------------------- | --------- | ----------------- | --------------------------------------------------------------------------------- |
| `--grpc-endpoint`    | `-e`      | `localhost:9090`  | gRPC endpoint to connect to                                                       |
| `--keyring-dir`      | `-k`      | `~/.celestia-app` | Directory containing the keyring                                                  |
| `--account`          | `-a`      | _(first account)_ | Account name to use from keyring                                                  |
| `--private-key`      | `-p`      | _(from keyring)_  | Private key hex (alternative to keyring, also via `CELESTIA_PRIVATE_KEY` env var) |
| `--blob-size`        | `-b`      | `1024`            | Maximum blob size in bytes                                                        |
| `--blob-size-min`    | `-z`      | `1`               | Minimum blob size in bytes                                                        |
| `--submission-delay` | `-d`      | `4000ms`          | Delay between transaction submissions                                             |
| `--namespace`        | `-n`      | `test`            | Namespace for blob submission                                                     |
| `--disable-metrics`  | `-m`      | `false`           | Disable metrics collection                                                        |

### Examples

```bash
# Run against Mocha testnet with 10KB blobs every second
cargo run --release -- -e [your-favorite-mocha-rpc]:9090 -a [your-account] -b 10000 -z 10000 -d 1000ms

# Run against talis using validator key
cargo run --release -- -e "[node-ip]:9090" -k [path-to-talis-dir/payload/validator-1] -b 104800 -z 104800 -d 150ms

# View help
cargo run --release -- --help
```

## Output

The tool provides:

1. Real-time logging for each transaction:
   - `[SUBMIT]` when a blob is broadcast (with tx hash, size, and timestamp)
   - `[CONFIRM]` when a transaction is confirmed (with tx hash, height, latency, code, and timestamp)
   - `[FAILED]` when confirmation fails (with tx hash and error)
   - `[CANCELLED]` when confirmation is cancelled due to shutdown
   - Status updates every 10 seconds showing the number of transactions submitted
2. A CSV file (`latency_results.csv`) containing:
   - Submit time
   - Commit time
   - Latency (in milliseconds)
   - Transaction hash
   - Block height
   - Transaction code
   - Failed status (true/false)
   - Error message (if failed)
3. Final statistics including:
   - Total number of transactions
   - Success/failure counts and percentages
   - Average latency (for successful transactions)
   - Standard deviation (for successful transactions)

## Stopping the Tool

Press `Ctrl+C` to stop the tool. The tool will wait for any in-flight confirmations to complete, then display final statistics and write the CSV file before exiting.

## Architecture

This tool uses a split submission/confirmation pattern:

- **Broadcast**: Transactions are broadcasted sequentially (required for proper nonce handling with a single signer)
- **Confirmation**: Transaction confirmations are tracked in parallel using a task pool (up to 100 concurrent confirmations)
- **Latency Measurement**: Time is measured from broadcast completion to block inclusion, excluding any queuing delays

## Notes

- The tool uses random data for blob content
- Each transaction broadcast is sequential, but confirmations are tracked in parallel
- The tool will continue running until interrupted
- Make sure your account has sufficient funds to handle the transaction rate
- Latency measurements exclude any time spent waiting in the confirmation queue
