# Blockstore Analyzer

A tool to analyze the blockstore database and verify if block parts were pruned.

## Purpose

This tool helps you:
- Check if pruning is active (base height > 0)
- Verify that block parts exist for specific heights
- Detect if block parts below the base height were properly pruned
- Count total block parts in the database
- Analyze blockstore statistics

## Building

```bash
cd tools/analyze-blockstore
go build -o analyze-blockstore
```

## Usage

### Basic Analysis

```bash
# Analyze default location (~/.celestia-app/data)
./analyze-blockstore

# Specify custom data directory
./analyze-blockstore -data-dir /path/to/data

# Use PebbleDB backend
./analyze-blockstore -db-type pebbledb
```

### Check Specific Height

```bash
# Check if block parts exist for a specific height
./analyze-blockstore -check-height 1000000
```

### Verbose Output

```bash
# Show detailed information about each block checked
./analyze-blockstore -verbose
```

## Output Interpretation

### Pruning Detected ✅
```
✅ Pruning detected! Base height is 997000 (not 0)
   This means blocks below height 997000 have been pruned.
```
This indicates that `min-retain-blocks` is working and old blocks have been pruned.

### No Pruning ⚠️
```
⚠️  No pruning detected (base = 0, all blocks from genesis are retained)
```
This means all blocks from genesis are still stored (no pruning active).

### Block Parts Status

- ✅ **Block parts found**: The block is retained and all parts are present
- ❌ **Block parts MISSING**: The block was pruned or partially pruned
- ⚠️ **Block meta NOT found**: The block was completely pruned

### Parts Below Base Height

If you see:
```
⚠️  Parts below base height (997000): 150 (should be 0 if pruning worked)
```

This indicates that some block parts were not properly deleted during pruning (possible bug or incomplete pruning).

## Examples

### Example 1: Verify Pruning is Working

```bash
$ ./analyze-blockstore -data-dir ~/.celestia-app/data

Analyzing blockstore at: /home/user/.celestia-app/data
Database type: goleveldb

=== Blockstore Statistics ===
Base height:   997000
Current height: 1000000
Number of blocks: 3001

✅ Pruning detected! Base height is 997000 (not 0)
   This means blocks below height 997000 have been pruned.

=== Block Part Verification ===

Checking base height 997000 (oldest retained block):
  ✅ Block meta found
  ✅ Block parts found (block is retained)
    Checked 2 sample parts, all present
  ✅ Full block can be loaded

Checking height 996999 (should be pruned, below base):
  ❌ Block meta NOT found (block was pruned or doesn't exist)

Checking current height 1000000 (latest block):
  ✅ Block meta found
  ✅ Block parts found (block is retained)
    Checked 2 sample parts, all present
  ✅ Full block can be loaded

=== Block Part Count Analysis ===
Total block parts found in database: 123456

Parts in expected range [997000, 1000000]: 123456
✅ No parts found below base height (pruning successful)
```

### Example 2: Check Specific Height

```bash
$ ./analyze-blockstore -check-height 500000 -verbose

Checking user-specified height 500000:
  ❌ Block meta NOT found (block was pruned or doesn't exist)
```

This confirms that height 500000 was pruned (it's below the base height).

## Troubleshooting

### Database Lock Error

If you get a database lock error, make sure the node is stopped:
```bash
# Stop the node first
sudo systemctl stop celestia-appd

# Then run the analyzer
./analyze-blockstore
```

### Wrong Database Type

If you're using PebbleDB but the tool defaults to LevelDB:
```bash
./analyze-blockstore -db-type pebbledb
```

### Permission Errors

Make sure you have read access to the data directory:
```bash
ls -la ~/.celestia-app/data/blockstore.db
```

## Integration with Monitoring

You can use this tool in scripts to monitor pruning:

```bash
#!/bin/bash
# Check if pruning is working
BASE=$(./analyze-blockstore -data-dir ~/.celestia-app/data 2>&1 | grep "Base height:" | awk '{print $3}')

if [ "$BASE" -gt 0 ]; then
    echo "✅ Pruning is active (base: $BASE)"
else
    echo "⚠️  Pruning is not active"
fi
```

## See Also

- [BLOCKSTORE_INVESTIGATION.md](../../BLOCKSTORE_INVESTIGATION.md) - Detailed investigation of blockstore contents
- [min-retain-blocks configuration](../../docs/configuration.md) - How to configure pruning
