# Celestia Latency Monitor

A tool for monitoring and measuring transaction latency in Celestia networks. This tool submits PayForBlob transactions at a specified rate and measures the time between submission and commitment, providing detailed latency statistics.

## Features

- Configurable transaction submission rate (in KB/s)
- Adjustable blob size
- Custom namespace support
- Real-time transaction monitoring
- Detailed latency statistics including mean and standard deviation
- CSV export of all transaction results

## Prerequisites

- A running Celestia node with gRPC enabled or find an existing endpoint
- Go 1.24 or later
- Access to a keyring with funds for transaction submission

## Usage

```bash
./latency-monitor [flags]
```

### Available Flags

- `-grpc-endpoint`: gRPC endpoint to connect to (default: "localhost:9090")
- `-keyring-dir`: Directory containing the keyring (default: "~/.celestia-app")
- `-submit-rate`: Data submission rate in KB/sec (default: 1.0)
- `-blob-size`: Size of blob data in bytes (default: 1024)
- `-namespace`: Namespace for blob submission (default: "test")

### Example

```bash
# Run with default settings (from the root directory)
go run ./tools/latency-monitor

# Run with custom settings
go run ./tools/latency-monitor -grpc-endpoint=localhost:9090 -submit-rate=2.0 -blob-size=2048 -namespace=custom
```

## Output

The tool provides:

1. Real-time updates every 10 seconds showing the number of transactions submitted
2. A CSV file (`latency_results.csv`) containing:
   - Submit time
   - Commit time
   - Latency (in milliseconds)
   - Transaction hash
   - Transaction code
3. Final statistics including:
   - Total number of transactions
   - Average latency
   - Standard deviation

## Stopping the Tool

Press `Ctrl+C` to stop the tool. The final statistics will be displayed and the CSV file will be written before the tool exits.

## Notes

- The tool uses random data for blob content
- Each transaction is submitted asynchronously
- The tool will continue running until interrupted
- Make sure your node has sufficient funds to handle the transaction rate
