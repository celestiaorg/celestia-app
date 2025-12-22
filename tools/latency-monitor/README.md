# Celestia Latency Monitor

A tool for monitoring and measuring transaction latency in Celestia networks. This tool submits PayForBlob transactions at a specified rate and measures the time between submission and commitment, providing detailed latency statistics.

## Features

- Configurable submission delay between transactions
- Random blob sizes between configurable minimum and maximum bounds
- Custom namespace support
- Real-time transaction monitoring
- Tracks both successful and failed confirmations
- Detailed latency statistics including mean and standard deviation
- CSV export of all transaction results with failure tracking

## Prerequisites

- A running Celestia node with gRPC enabled or find an existing endpoint
- Go 1.24 or later
- Access to a keyring with funds for transaction submission

## Usage

```bash
./latency-monitor [flags]
```

### Available Flags

| Flag | Shorthand | Default | Description |
|------|-----------|---------|-------------|
| `--grpc-endpoint` | `-e` | `localhost:9090` | gRPC endpoint to connect to |
| `--keyring-dir` | `-k` | `~/.celestia-app` | Directory containing the keyring |
| `--account` | `-a` | _(first account)_ | Account name to use from keyring |
| `--blob-size` | `-b` | `1024` | Maximum blob size in bytes (blobs will be random size between this value and the minimum) |
| `--blob-size-min` | `-z` | `1` | Minimum blob size in bytes (blobs will be random size between this value and the maximum) |
| `--submission-delay` | `-d` | `4000ms` | Delay between transaction submissions |
| `--namespace` | `-n` | `test` | Namespace for blob submission |
| `--disable-metrics` | `-m` | `false` | Disable metrics collection |

### Example

```bash
# Run with default settings (from the root directory)
go run ./tools/latency-monitor

# Run with custom settings (long flags)
go run ./tools/latency-monitor --grpc-endpoint localhost:9090 --submission-delay 200ms --blob-size 4096 --blob-size-min 1024 --namespace custom

# Run with custom settings (short flags)
go run ./tools/latency-monitor -e localhost:9090 -d 200ms -b 4096 -z 1024 -n custom

# Use a specific account from keyring
go run ./tools/latency-monitor -a validator

# View help
go run ./tools/latency-monitor --help
```

## Output

The tool provides:

1. Real-time logging for each transaction:
   - `[SUBMIT]` when a blob is broadcast (with tx hash, size, and timestamp)
   - `[CONFIRM]` when a transaction is confirmed (with tx hash, height, latency, code, and timestamp)
   - `[FAILED]` when confirmation fails (with tx hash and error)
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

Press `Ctrl+C` to stop the tool. The final statistics will be displayed and the CSV file will be written before the tool exits.

## Notes

- The tool uses random data for blob content
- Each transaction is submitted asynchronously
- The tool will continue running until interrupted
- Make sure your node has sufficient funds to handle the transaction rate
