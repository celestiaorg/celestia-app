# Blockstore.db Investigation

## Overview

`blockstore.db` is a LevelDB database that stores all block data for the Celestia blockchain. It is managed by CometBFT (formerly Tendermint) and contains the complete block history from genesis to the current height.

## What Gets Saved to blockstore.db

Based on the code in `celestia-core/store/store.go`, the blockstore saves the following data for each block:

### 1. **Block Parts** (Largest Component)
- **Key format**: `P:{height}:{partIndex}`
- **Size**: Each part is **64 KB** (`BlockPartSizeBytes = 65536 bytes`)
- **Content**: Serialized block data split into parts
- **Number of parts**: Depends on block size
  - For a 128 MB max block: up to ~1,953 parts (128MB / 64KB + 1)
  - For an 8 MB block: up to ~123 parts
- **Why it's large**: Blocks contain all transaction data, including blob data for data availability

### 2. **Block Metadata**
- **Key format**: `H:{height}`
- **Content**: BlockMeta containing:
  - BlockID (hash + PartSetHeader)
  - Block size
  - Block header
  - Number of transactions
- **Size**: Relatively small (~few KB per block)

### 3. **Block Hash Index**
- **Key format**: `BH:{hash}`
- **Content**: Height as string (for reverse lookup)
- **Size**: Very small

### 4. **Block Commit**
- **Key format**: `C:{height}`
- **Content**: The commit from validators (precommit votes) for the previous block
- **Size**: Depends on validator set size (~few KB per block)

### 5. **Seen Commit**
- **Key format**: `SC:{height}`
- **Content**: The +2/3 precommits that were seen for this block
- **Size**: Similar to block commit
- **Note**: This can be deleted at a later height (see pruning)

### 6. **Extended Commit** (if vote extensions enabled)
- **Key format**: `EC:{height}`
- **Content**: Extended commit with vote extension data
- **Size**: Larger than regular commit due to extension data

### 7. **Transaction Info** (optional)
- **Key format**: `TH:{txHash}`
- **Content**: Execution results for transactions (only error logs for failed txs)
- **Size**: Small per transaction

### 8. **BlockStore State**
- **Key**: `blockStore`
- **Content**: Base height and current height
- **Size**: Very small

## Why blockstore.db is So Large

### 1. **Large Block Sizes**
Celestia blocks can be very large:
- **Maximum block size**: 128 MB (DefaultMaxBlockSizeBytes)
- **Current governance limit**: ~8 MB (with square size 128)
- **Block parts**: Each 64 KB part is stored separately
- **Example**: An 8 MB block = ~123 parts × 64 KB = ~8 MB of block data alone

### 2. **Complete Block History** (if `min-retain-blocks = 0`)
The blockstore maintains **all contiguous blocks** from `base` to `height`:
- **Default behavior**: No automatic pruning (`min-retain-blocks = 0` by default)
- Every block's complete data is stored
- For a chain with 1 million blocks at 1 MB average:
  - Total size ≈ 1 TB
- **With pruning**: If `min-retain-blocks > 0`, only recent blocks are retained

### 3. **Data Duplication**
- Block parts contain the full serialized block (including commits)
- Commits are also stored separately (`C:` and `SC:` keys)
- This creates some duplication, though commits are relatively small

### 4. **Celestia-Specific: Blob Data**
Celestia blocks contain:
- **Transactions**: Regular Cosmos SDK transactions
- **Blobs**: Large data blobs for data availability (up to 2 MB per transaction)
- **Square data**: All data is organized into a data square for erasure coding
- This makes Celestia blocks significantly larger than typical blockchain blocks

### 5. **Database Overhead**
LevelDB has overhead:
- Key-value storage overhead
- Index structures
- Write-ahead logs
- SSTable files

## Storage Calculation Example

For a typical Celestia mainnet block:
- **Block size**: ~2-4 MB (average)
- **Number of parts**: ~30-60 parts (2-4 MB / 64 KB)
- **Block parts storage**: ~2-4 MB
- **Metadata + commits**: ~10-20 KB
- **Total per block**: ~2-4 MB

For 1 million blocks:
- **Total size**: ~2-4 TB

## Pruning

The blockstore supports pruning via `PruneBlocks()`. **Yes, `min-retain-blocks` DOES prune block parts automatically!**

### Automatic Pruning via `min-retain-blocks`

The `min-retain-blocks` setting in `app.toml` automatically prunes block parts from the blockstore:

1. **Configuration**: Set `min-retain-blocks` in `app.toml` (default: `0` = no pruning)
2. **Calculation**: BaseApp calculates retention height: `retentionHeight = commitHeight - minRetainBlocks`
3. **ABCI Response**: Retention height is returned in `ResponseCommit.RetainHeight`
4. **Automatic Pruning**: CometBFT automatically calls `blockStore.PruneBlocks(retainHeight, state)` after each commit
5. **Block Parts Deleted**: All block parts below the retention height are deleted

**Example**: If `min-retain-blocks = 3000` and current height is 1,000,000:
- Retention height = 1,000,000 - 3,000 = 997,000
- All blocks below height 997,000 are pruned (including all their block parts)

### What Gets Pruned

When pruning occurs, the following are deleted:
- **All block parts** (`P:{height}:{index}`) - **This is the largest component!**
- Block metadata (`H:{height}`) - except if within evidence retention period
- Block commits (`C:{height}`) - except if within evidence retention period
- Seen commits (`SC:{height}`)
- Transaction info (`TH:{hash}`)
- Block hash index (`BH:{hash}`)

### What is Protected

- Blocks within the evidence retention period (for proving malicious behavior)
- Header and commit data for evidence purposes (even if block parts are pruned)

### Manual Pruning

Pruning can also be triggered manually via the `PruneBlocks()` method:

```go
func (bs *BlockStore) PruneBlocks(height int64, state sm.State) (uint64, int64, error)
```

### Code Flow

1. `cosmos-sdk/baseapp/abci.go:Commit()` → calculates `retainHeight = GetBlockRetentionHeight(commitHeight)`
2. Returns `RetainHeight` in `ResponseCommit`
3. `celestia-core/state/execution.go:Commit()` → gets `res.RetainHeight`
4. `celestia-core/state/execution.go:ExecuteBlock()` → calls `pruneBlocks(retainHeight, state)` if `retainHeight > 0`
5. `celestia-core/store/store.go:PruneBlocks()` → deletes all block parts for heights < `retainHeight`

## Location

The blockstore database is located at:
- **Path**: `{dataDir}/data/blockstore.db/` (it's a directory, not a single file)
- **Config**: Set via `blockstore_dir` in `config.toml` (defaults to `db_dir`)

## Monitoring

The application tracks blockstore size via Prometheus metrics:
- **Metric**: `celestia_app_disk_space_bytes{database="blockstore.db"}`
- **Location**: `app/metrics/disk_space.go`
- **Update frequency**: Every 15 seconds

## Recommendations

1. **Monitor blockstore size** using the Prometheus metrics
2. **Use `min-retain-blocks`** to control blockstore size:
   - Set `min-retain-blocks` in `app.toml` to automatically prune old blocks
   - Example: `min-retain-blocks = 3000` keeps only the last 3000 blocks
   - This will automatically prune all block parts below the retention height
   - **Note**: Setting this too low may prevent state sync and light client verification
3. **Consider blockstore location**: Can be on separate storage if needed
4. **Archive nodes**: Keep `min-retain-blocks = 0` to retain full history
5. **Pruned nodes**: Set `min-retain-blocks` based on your needs (e.g., 3000-10000 blocks)

## Analyzing Blockstore Pruning

### Using the Blockstore Analyzer Tool

A tool is available to verify if block parts were pruned:

```bash
cd tools/analyze-blockstore
go build -o analyze-blockstore
./analyze-blockstore -data-dir ~/.celestia-app/data
```

**What it checks:**
- Base height vs current height (indicates if pruning is active)
- Verifies block parts exist for specific heights
- Detects if parts below base height were properly pruned
- Counts total block parts in database
- Shows detailed statistics

**Example output:**
```
✅ Pruning detected! Base height is 997000 (not 0)
   This means blocks below height 997000 have been pruned.

Checking height 996999 (should be pruned, below base):
  ❌ Block meta NOT found (block was pruned or doesn't exist)

✅ No parts found below base height (pruning successful)
```

See `tools/analyze-blockstore/README.md` for detailed usage.

### Manual Verification Methods

1. **Check Base Height via RPC:**
   ```bash
   curl http://localhost:26657/status | jq '.result.sync_info.earliest_block_height'
   ```
   If this is > 0, pruning is active.

2. **Check Blockstore State:**
   The blockstore state key `blockStore` contains base and height. You can inspect this using LevelDB tools.

3. **Try Loading Old Blocks:**
   ```bash
   # If a block below base height cannot be loaded, it was pruned
   curl http://localhost:26657/block?height=1000
   ```

## References

- BlockStore implementation: `celestia-core/store/store.go`
- Block structure: `celestia-core/types/block.go`
- Block part size: `celestia-core/types/params.go` (64 KB)
- Pruning logic: `celestia-core/store/store.go:PruneBlocks()`
- Disk space metrics: `celestia-app/app/metrics/disk_space.go`
- Blockstore analyzer: `celestia-app/tools/analyze-blockstore/`
