# migrate-db

A tool to migrate Celestia App databases from LevelDB to PebbleDB.

## Overview

This tool migrates all celestia-app databases from LevelDB to PebbleDB format:

- **Resumable**: If interrupted (Ctrl-C, crash, reboot), re-run to continue from where it left off
- **Idempotent**: Running it again after completion is a no-op
- **Storage-efficient**: Source data is deleted incrementally as it's migrated (default)
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
2. Migrate all databases to PebbleDB format, deleting source data incrementally
3. Auto-swap PebbleDB files into `data/` and update `config.toml`

### Options

| Flag                   | Default           | Description                                            |
|------------------------|-------------------|--------------------------------------------------------|
| `--home <path>`        | `~/.celestia-app` | Node home directory                                    |
| `--dry-run`            | `false`           | Test without making changes                            |
| `--backup`             | `false`           | Keep source LevelDB data after migration               |
| `--batch-size <MB>`    | `64`              | Write batch size in MB                                 |
| `--sync-interval <MB>` | `1024`            | Fsync every N MB (0 = sync only at DB end)             |
| `--parallel <N>`       | `3`               | Migrate N databases concurrently (max 5)               |
| `--verify`             | `false`           | Run sample verification after migration                |
| `--db <name>`          | all               | Migrate only a specific database                       |
| `--manual-swap`        | `false`           | Skip auto-swap; print manual instructions instead      |

### Examples

**Recommended:**

```bash
./migrate-db
```

Uses all defaults: deletes source data incrementally to save disk space, auto-swaps PebbleDB files into `data/` when done, and updates `config.toml`. No manual steps needed after completion.

**Dry-run:**

```bash
./migrate-db --dry-run
```

**Fast migration with all resources (parallel, minimal syncing):**

```bash
./migrate-db --parallel 5 --sync-interval 0
```

**Keep source data as backup:**

```bash
./migrate-db --backup --manual-swap
```

**Migrate a single database:**

```bash
./migrate-db --db blockstore
```

**Migration with post-migration verification:**

```bash
./migrate-db --verify
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
./migrate-db
```

The tool will:

1. Detect the existing `data_pebble/` directory and `.migration_state.json`
2. Skip databases that are already complete
3. For in-progress databases, find the last migrated key and verify it was written correctly
4. Continue from the next key after the verified resume point

Progress is tracked at two levels:

- **Per-database**: A state file tracks which databases are complete
- **Per-key**: The last key in each PebbleDB is the durable checkpoint (no separate tracking needed)

On resume, the tool verifies the last written key by comparing its value in both the source and destination databases. If the source key was already deleted (default no-backup mode), the verification is skipped.

## Complete Migration Process

### 1. Stop Your Node

```bash
sudo systemctl stop celestia-appd
```

### 2. Run Migration

```bash
cd /path/to/celestia-app/tools/migrate-db
go build -o migrate-db
./migrate-db
```

By default, this deletes source data incrementally and auto-swaps the databases into place.

### 3. Start and Verify

```bash
sudo systemctl start celestia-appd
celestia-appd status
journalctl -u celestia-appd -f
```

### 4. Cleanup

```bash
rm -rf ~/.celestia-app/data_pebble
```

## Manual Swap (--manual-swap)

If you used `--manual-swap`, follow the printed instructions to move databases and update config:

```bash
cd ~/.celestia-app

# Remove old databases (skip if source was already deleted)
rm -rf data/application.db data/blockstore.db data/state.db data/tx_index.db data/evidence.db

# Move PebbleDB files
mv data_pebble/application.db data/application.db
mv data_pebble/blockstore.db data/blockstore.db
mv data_pebble/state.db data/state.db
mv data_pebble/tx_index.db data/tx_index.db
mv data_pebble/evidence.db data/evidence.db
```

Edit `~/.celestia-app/config/config.toml`:

```toml
db_backend = "pebbledb"
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

- **Default mode**: ~1x + 1GB overhead (source keys deleted incrementally every ~1GB)
- **`--backup` mode**: ~2x your data size (source + destination side-by-side)

```bash
du -sh ~/.celestia-app/data
df -h ~/.celestia-app
```

## Performance Tuning

- **`--parallel 5`**: Migrate all 5 databases concurrently (if I/O bandwidth allows)
- **`--sync-interval 0`**: No intermediate fsyncs (fastest, but more re-work on crash)
- **`--batch-size 256`**: Larger batches (uses more memory, may improve throughput)
