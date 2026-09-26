# check-eds-construction

`check-eds-construction` verifies that the EDS construction method produces consistent data roots, comparing construction with and without tree pool optimization against block data hashes. It fetches blocks via a node's RPC, reconstructs the EDS using both methods, computes the DAH, and compares all hashes.

## Usage

Build or run with Go:

```bash
# Build the tool
go build -o check-eds-construction ./tools/check-eds-construction

# Or run directly with go run
go run ./tools/check-eds-construction [command]
```

### Commands

#### Check a specific block

```bash
./check-eds-construction --rpc <node_rpc> check <height>
```

- `--rpc`: CometBFT HTTP RPC endpoint (e.g., `http://localhost:26657` or `https://rpc-mocha.pops.one:443`)
- `<height>`: Height of the block to verify

#### Check random blocks

```bash
./check-eds-construction --rpc <node_rpc> random [n] [--delay <ms>]
```

- `--rpc`: CometBFT HTTP RPC endpoint
- `[n]`: Number of random blocks to check (optional, defaults to 10)
- `--delay`: Delay between block checks in milliseconds (optional, defaults to 100ms)

### Examples

```bash
# Check a specific block
$ go run ./tools/check-eds-construction --rpc https://rpc-mocha.pops.one:443 check 1134818
Connected to https://rpc-mocha.pops.one:443 on chain mocha-5
Got data root: 7F7EC5311B2E62430D95AA55E22BF8EED15CFD826C87336DD40C1D8C2AF12057
Computed data root: 7F7EC5311B2E62430D95AA55E22BF8EED15CFD826C87336DD40C1D8C2AF12057
Computed data root (with pool): 7F7EC5311B2E62430D95AA55E22BF8EED15CFD826C87336DD40C1D8C2AF12057
All roots match!

# Check 2 random blocks with default 100ms delay
$ go run ./tools/check-eds-construction --rpc https://rpc-mocha.pops.one:443 random 2
Connected to https://rpc-mocha.pops.one:443 on chain mocha-5
Latest block height: 1134830

Checking 2 random blocks with 100ms delay between checks...

[1/2] Checking block at height 221751
Got data root: 3D96B7D238E7E0456F6AF8E7CDF0A67BD6CF9C2089ECB559C659DCAA1F880353
Computed data root: 3D96B7D238E7E0456F6AF8E7CDF0A67BD6CF9C2089ECB559C659DCAA1F880353
Computed data root (with pool): 3D96B7D238E7E0456F6AF8E7CDF0A67BD6CF9C2089ECB559C659DCAA1F880353
All roots match!
Block 221751 passed

[2/2] Checking block at height 723681
Got data root: 617D95601B2F44B4EAD04CEF545A9CB44F5A91A3A531B1C11DB7195E34176806
Computed data root: 617D95601B2F44B4EAD04CEF545A9CB44F5A91A3A531B1C11DB7195E34176806
Computed data root (with pool): 617D95601B2F44B4EAD04CEF545A9CB44F5A91A3A531B1C11DB7195E34176806
All roots match!
Block 723681 passed

# Check 10 random blocks (default) with custom 500ms delay
$ go run ./tools/check-eds-construction --rpc https://rpc-mocha.pops.one:443 random --delay 500

# Check 10 random blocks with no delay
$ go run ./tools/check-eds-construction --rpc https://rpc-mocha.pops.one:443 random --delay 0
```

### Help

```bash
# Show general help
./check-eds-construction --help

# Show help for a specific command
./check-eds-construction check --help
./check-eds-construction random --help
```

## What It Does

The tool performs the following for each block:

1. Fetches the block from the specified RPC endpoint
2. Constructs the EDS using the standard method (without tree pool)
3. Constructs the EDS using the optimized method (with preallocated tree pool)
4. Compares both computed data roots with each other and with the block's data hash
5. Reports whether all hashes match

## Notes

- Requires access to a live node's RPC endpoint
- The tool uses the block's App version when reconstructing the EDS
- For `random` mode, blocks are selected randomly from height 2 to the latest block height
- Ensure the target height exists and the node is synced
