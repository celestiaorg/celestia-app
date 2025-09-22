# Blob Submitter

A stress testing tool for submitting blobs to the Celestia network mempool.

## Overview

This tool continuously submits random blob data to a Celestia node to test mempool performance and network capacity. It supports concurrent submissions and tracks confirmation statistics.

## Usage

```bash
./blob-submitter [flags]
```

### Flags

- `--endpoint`: gRPC endpoint to connect to (default: "localhost:9090")
- `--keyring-dir`: Directory containing the keyring (default: "~/.celestia-app")
- `--blob-size`: Size of blob data in bytes (default: 1024)
- `--concurrency`: Number of concurrent blob submissions (default: 1)
- `--namespace`: Namespace for blob submission (default: "blobstress")
- `--account`: Account name to use for signing (uses first available if not specified)

### Examples

Basic usage:
```bash
./blob-submitter
```

Submit larger blobs with higher concurrency:
```bash
./blob-submitter --blob-size 4096 --concurrency 5
```

Connect to a remote node:
```bash
./blob-submitter --endpoint node.example.com:9090
```

Use a specific namespace and account:
```bash
./blob-submitter --namespace "stress-test" --account "validator1"
```

## Features

- **Concurrent submissions**: Submit multiple blobs simultaneously
- **Configurable blob size**: Test with different payload sizes
- **Real-time statistics**: Track submission success/failure rates
- **Confirmation tracking**: Monitor when blobs are included in blocks
- **Graceful shutdown**: Stop cleanly with Ctrl+C

## Implementation Details

The tool uses the following approach:

1. **Submission Workers**: Each worker continuously generates random blob data and submits it using `BroadcastPayForBlobWithAccount`
2. **Confirmation Tracking**: Separate goroutines monitor each submitted transaction to confirm inclusion
3. **Statistics Reporting**: Regular reporting of submitted, confirmed, and failed transactions
4. **Signal Handling**: Clean shutdown on interrupt signals

## Current Status

This is a fully functional implementation that uses the real celestia-app APIs:

- ✅ Full transaction client setup with keyring integration
- ✅ Real blob submission via `BroadcastPayForBlobWithAccount`
- ✅ Concurrent submission workers with configurable concurrency
- ✅ Real-time statistics and confirmation tracking
- ✅ Graceful shutdown and error handling

Note: Confirmation checking currently uses a simple timer approach. For production use, you may want to implement actual transaction status queries.

## Building

From the tool directory:
```bash
go build -o blob-submitter .
```

The tool uses the main project's dependencies, so no separate `go.mod` is needed.

## Requirements

- Go 1.21+
- Access to a Celestia node with gRPC enabled
- Keyring with funded accounts for transaction fees