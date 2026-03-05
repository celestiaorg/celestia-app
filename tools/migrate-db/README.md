# migrate-db

A tool to migrate Celestia App databases from LevelDB to PebbleDB.

## Overview

This tool migrates all celestia-app databases from LevelDB to PebbleDB format:

- **Resumable**: If interrupted (Ctrl-C, crash, reboot), re-run to continue from where it left off
- **Idempotent**: Running it again after completion is a no-op
- **Storage-efficient**: `--no-backup` mode deletes source data incrementally as it's migrated
- **Parallel**: Migrate multiple databases concurrently with `--parallel`

## Installation

```bash
cd tools/migrate-db
go build -o migrate-db
```

## Usage

### Basic Migration

```bash
./migrate-db
```

This will:

1. Create a `data_pebble` directory in `~/.celestia-app/`
2. Migrate all databases to PebbleDB format
3. Verify integrity via sampling
4. Display instructions for completing the migration

### Options

| Flag                   | Default           | Description                                                             |
|------------------------|-------------------|-------------------------------------------------------------------------|
| `--home <path>`        | `~/.celestia-app` | Node home directory                                                     |
| `--dry-run`            | `false`           | Test without making changes                                             |
| `--no-backup`          | `false`           | Delete source data incrementally as it's migrated                       |
| `--batch-size <MB>`    | `64`              | Write batch size in MB                                                  |
| `--sync-interval <MB>` | `1024`            | Fsync every N MB (0 = sync only at DB end)                              |
| `--parallel <N>`       | `3`               | Migrate N databases concurrently (max 5)                                |
| `--verify-full`        | `false`           | Exhaustive key-count verification instead of sampling                   |
| `--skip-verify`        | `false`           | Skip post-migration verification                                        |
| `--db <name>`          | all               | Migrate only a specific database                                        |
| `--auto-swap`          | `false`           | Automatically move PebbleDB files into `data/` and update `config.toml` |

### Examples

**Dry-run:**

```bash
./migrate-db --dry-run
```

**Fast migration with all resources (parallel, minimal syncing):**

```bash
./migrate-db --parallel 5 --sync-interval 0 --skip-verify
```

**Storage-efficient migration (deletes source data as it goes):**

```bash
./migrate-db --no-backup --parallel 3
```

**Migrate a single database:**

```bash
./migrate-db --db blockstore
```

**Full automated migration (including swap and config update):**

```bash
./migrate-db --no-backup --parallel 3 --auto-swap
```

## Migrated Databases

- `application.db` - Application state (usually the largest)
- `blockstore.db` - Block storage
- `state.db` - Consensus state
- `tx_index.db` - Transaction index
- `evidence.db` - Evidence storage

Files NOT migrated (remain unchanged): `cs.wal`, `priv_validator_state.json`, `snapshots/`, `traces/`

## Resuming Interrupted Migrations

The tool is fully resumable. If a migration is interrupted for any reason:

```bash
# Just re-run the same command
./migrate-db --no-backup --parallel 3
```

The tool will:
1. Detect the existing `data_pebble/` directory and `.migration_state.json`
2. Skip databases that are already complete
3. For in-progress databases, find the last migrated key and continue from there

Progress is tracked at two levels:
- **Per-database**: A state file tracks which databases are complete
- **Per-key**: The last key in each PebbleDB is the durable checkpoint (no separate tracking needed)

## Complete Migration Process

### 1. Stop Your Node

```bash
sudo systemctl stop celestia-appd
```

### 2. Run Migration

```bash
cd /path/to/celestia-app/tools/migrate-db
go build -o migrate-db
./migrate-db --no-backup --parallel 3
```

### 3. Swap Databases

If you used `--auto-swap`, this is **done automatically**. Otherwise:

```bash
cd ~/.celestia-app

# Remove old databases (skip if --no-backup already deleted them)
rm -rf data/application.db data/blockstore.db data/state.db data/tx_index.db data/evidence.db

# Move PebbleDB files
mv data_pebble/application.db data/application.db
mv data_pebble/blockstore.db data/blockstore.db
mv data_pebble/state.db data/state.db
mv data_pebble/tx_index.db data/tx_index.db
mv data_pebble/evidence.db data/evidence.db
```

### 4. Update Configuration

Edit `~/.celestia-app/config/config.toml`:

```toml
db_backend = "pebbledb"
```

### 5. Start and Verify

```bash
sudo systemctl start celestia-appd
celestia-appd status
journalctl -u celestia-appd -f
```

### 6. Cleanup

```bash
rm -rf ~/.celestia-app/data_pebble
```

## Crash Recovery

| Scenario                        | What Happens                                                       |
|---------------------------------|--------------------------------------------------------------------|
| Ctrl-C mid-batch                | Uncommitted batch is lost. Re-run resumes from last committed key. |
| Power loss                      | At most `--sync-interval` MB of data re-migrated on restart.       |
| Crash during source deletion    | Source keys already in PebbleDB. Re-run skips them.                |
| `data_pebble/` manually deleted | Fresh start — no state to resume from.                             |
| Crash while lock held           | Kernel automatically releases file lock — no stale lock possible.  |

## Disk Space Requirements

- **Default mode**: ~2x your data size (source + destination side-by-side)
- **`--no-backup` mode**: ~1x + 1GB overhead (source keys deleted incrementally every ~1GB)

```bash
du -sh ~/.celestia-app/data
df -h ~/.celestia-app
```

## Performance Tuning

- **`--parallel 5`**: Migrate all 5 databases concurrently (if I/O bandwidth allows)
- **`--sync-interval 0`**: No intermediate fsyncs (fastest, but more re-work on crash)
- **`--batch-size 256`**: Larger batches (uses more memory, may improve throughput)
- **`--skip-verify`**: Skip verification step for maximum speed
