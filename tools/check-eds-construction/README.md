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

- `--rpc`: CometBFT HTTP RPC endpoint (e.g., `http://localhost:26657` or `https://rpc.celestia-arabica-11.com:443`)
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
$ go run ./tools/check-eds-construction --rpc https://rpc.celestia-arabica-11.com:443 check 5165052
Connected to https://rpc.celestia-arabica-11.com:443 on chain arabica-11
Got data root: 3C7F...ABCD
Computed data root: 3C7F...ABCD
Computed data root (with pool): 3C7F...ABCD
All roots match!

# Check 20 random blocks with default 100ms delay
$ go run ./tools/check-eds-construction --rpc https://rpc.celestia-arabica-11.com:443 random 20
Connected to https://rpc.celestia-arabica-11.com:443 on chain arabica-11
Latest block height: 5165100

Checking 20 random blocks with 100ms delay between checks...

[1/20] Checking block at height 4932156
Got data root: ABC1...2345
Computed data root: ABC1...2345
Computed data root (with pool): ABC1...2345
All roots match!
Block 4932156 passed

[2/20] Checking block at height 5012437
...

# Check 10 random blocks (default) with custom 500ms delay
$ go run ./tools/check-eds-construction --rpc https://rpc.celestia-arabica-11.com:443 random --delay 500

# Check 10 random blocks with no delay
$ go run ./tools/check-eds-construction --rpc https://rpc.celestia-arabica-11.com:443 random --delay 0
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
