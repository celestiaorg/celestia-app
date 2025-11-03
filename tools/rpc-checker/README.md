# Celestia RPC Checker

A tool for checking block_results availability across multiple Celestia archive RPC providers. This tool is designed to help debug data availability issues by querying multiple archive nodes and reporting which ones successfully return data for a specific block height.

## Purpose

This tool was created to help debug issues like [#5390](https://github.com/celestiaorg/celestia-app/issues/5390) where L2 integrators reported problems syncing historical blob data. It queries the `/block_results?height=X` endpoint across multiple archive RPC providers and generates a report showing which endpoints are working correctly.

## Features

- Concurrent querying of multiple RPC endpoints for efficiency
- Configurable block height to query
- Configurable timeout per request
- Support for custom endpoint lists
- Detailed results table showing success/failure, latency, and error messages
- Summary statistics

## Prerequisites

- Go 1.21 or later

## Usage

```bash
# Run with default settings (queries height 1034505 from default endpoint list)
go run ./tools/rpc-checker

# Query a different block height
go run ./tools/rpc-checker --height 1000000

# Use custom timeout
go run ./tools/rpc-checker --timeout 30s

# Query custom endpoints
go run ./tools/rpc-checker --endpoints https://rpc1.example.com,https://rpc2.example.com

# Combine options
go run ./tools/rpc-checker --height 500000 --timeout 15s
```

### Available Flags

| Flag | Shorthand | Default | Description |
|------|-----------|---------|-------------|
| `--height` | `-b` | `1034505` | Block height to query |
| `--timeout` | `-t` | `10s` | Timeout for each request |
| `--endpoints` | `-e` | _(default list)_ | Custom list of RPC endpoints (comma-separated) |

### Default Endpoints

The tool includes a curated list of Celestia mainnet archive RPC endpoints from:
- https://celestia-tools.brightlystake.com/rpc-status
- https://itrocket.net/services/mainnet/celestia/public-rpc/
- Additional known archive endpoints

## Output

The tool provides:

1. A results table showing for each endpoint:
   - Endpoint URL
   - Status (✓ SUCCESS or ✗ FAILED)
   - Latency (in milliseconds)
   - Error message (if failed)

2. Summary statistics:
   - Total successful endpoints
   - Total failed endpoints
   - Success/failure percentages

### Example Output

```
Checking block_results at height 1034505 across 15 RPC providers
Request timeout: 10s

Results: 12/15 endpoints succeeded

Endpoint                                                     | Status     | Latency    | Error
------------------------------------------------------------------------------------------------------------------------------------------------
https://celestia-archive-rpc.rpc-archive.stakewith.us       | ✓ SUCCESS  | 245ms      | -
https://celestia-mainnet-rpc.autostake.com                  | ✓ SUCCESS  | 312ms      | -
https://celestia-mainnet-rpc.itrocket.net                   | ✓ SUCCESS  | 189ms      | -
https://celestia-rpc.brightlystake.com                      | ✗ FAILED   | 5012ms     | request failed: context deadline exceeded
https://celestia-rpc.easy2stake.com                         | ✓ SUCCESS  | 456ms      | -
https://celestia-rpc.kjnodes.com                            | ✓ SUCCESS  | 278ms      | -
https://celestia-rpc.lavenderfive.com                       | ✓ SUCCESS  | 198ms      | -
https://celestia-rpc.mesa-nodes.com                         | ✓ SUCCESS  | 334ms      | -
https://celestia-rpc.openbitlab.com                         | ✓ SUCCESS  | 412ms      | -
https://celestia.archive.rpc.stakewith.us                   | ✓ SUCCESS  | 267ms      | -
https://celestia.rpc.kjnodes.com                            | ✗ FAILED   | 89ms       | RPC error -32603: Internal error
https://public-celestia-rpc.numia.xyz                       | ✓ SUCCESS  | 523ms      | -
https://rpc-celestia-archive.trusted-point.com              | ✓ SUCCESS  | 445ms      | -
https://rpc-celestia.cosmos-spaces.cloud                    | ✗ FAILED   | 156ms      | HTTP 404: not found
https://rpc-celestia.whispernode.com                        | ✓ SUCCESS  | 389ms      | -

Summary:
  Successful: 12 (80.0%)
  Failed:     3 (20.0%)
```

## Use Cases

- Debugging historical data availability issues
- Monitoring archive node health across providers
- Identifying which RPC providers have complete historical data
- Testing RPC endpoint reliability for specific block heights

## Notes

- The tool queries endpoints concurrently for efficiency
- Each request has an individual timeout (default 10s)
- Failed requests will show the specific error (timeout, HTTP error, RPC error, etc.)
- The default endpoint list may be updated over time as providers change
