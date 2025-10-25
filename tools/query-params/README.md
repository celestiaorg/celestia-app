# Query Params Tool

A command-line tool to query all module parameters from a Cosmos SDK-based chain (v0.46 and v0.50) at a specific block height.

## Overview

This tool connects to a Cosmos SDK chain via gRPC and queries parameters from all modules at a given height. It first retrieves the block header to determine the app version, then uses that information to query only the modules that exist in that version.

## Features

- Query parameters from all modules at a specific height or latest height
- Supports querying archive nodes for historical state
- Automatically detects app version from block header
- Queries both standard Cosmos SDK modules and Celestia-specific modules
- Outputs in JSON or human-readable text format

## Building

From this directory:

```bash
go build -o query-params .
```

Or from the root of the celestia-app repo:

```bash
go build -o ./bin/query-params ./tools/query-params
```

## Usage

Basic usage:

```bash
# Query latest height
./query-params -grpc localhost:9090

# Query specific height
./query-params -grpc localhost:9090 -height 1000000

# Output as text instead of JSON
./query-params -grpc localhost:9090 -height 1000000 -output text

# Query a remote node
./query-params -grpc consensus.lunaroasis.net:9090 -height 2000000
```

### Flags

- `-grpc`: gRPC endpoint address (default: "localhost:9090")
- `-height`: Block height to query. Use 0 for latest height (default: 0)
- `-output`: Output format: "json" or "text" (default: "json")

## Modules Queried

The tool queries parameters from the following modules (based on app version):

**Cosmos SDK Standard Modules:**
- auth
- bank
- staking
- slashing
- distribution
- gov
- mint
- consensus (v4+)

**IBC Modules:**
- ibc-transfer
- ica-host

**Celestia-Specific Modules:**
- blob
- minfee (v4+)

Note: The celestia `mint` module uses the standard Cosmos SDK mint parameters.

## Output Format

### JSON Output (default)

```json
{
  "height": 1000000,
  "app_version": 5,
  "chain_id": "celestia",
  "time": "2024-01-01T00:00:00Z",
  "params": {
    "auth": { ... },
    "bank": { ... },
    "staking": { ... },
    ...
  }
}
```

### Text Output

```
Height: 1000000
App Version: 5
Chain ID: celestia

=== Module Parameters ===

Module: auth
  {
    "max_memo_characters": "256",
    ...
  }

Module: bank
  {
    "default_send_enabled": true,
    ...
  }
...
```

## Notes

- The tool requires a gRPC endpoint with query access
- Some modules may not be available in older app versions
- Failed module queries will log warnings to stderr but won't stop execution
- Historical queries require an archive node with state at the requested height

## Example Use Cases

1. **Compare parameters across upgrades:**
   ```bash
   ./query-params -height 1000000 > before.json
   ./query-params -height 2000000 > after.json
   diff before.json after.json
   ```

2. **Audit current chain configuration:**
   ```bash
   ./query-params -grpc mainnet.celestia.org:9090 -output text
   ```

3. **Debug parameter changes:**
   ```bash
   ./query-params -height 1234567 | jq '.params.staking'
   ```
