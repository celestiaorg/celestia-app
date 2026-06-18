# migrate-db

A tool to migrate Celestia App databases from LevelDB to PebbleDB.

## Overview

This tool migrates all of a node's databases from LevelDB to PebbleDB and then
flips the node's configured backend. It is:

- **Resumable**: If interrupted (Ctrl-C, crash, reboot), re-run to continue from where it left off
- **Verified**: Every destination database is reopened and fully read back (PebbleDB's own consistency check + a content comparison) **before** the source is destroyed or swapped into place
- **Crash-safe**: The swap moves the old database aside (not delete) and only removes it after the new database is verified in place and the config is updated; a failure rolls back
- **Storage-efficient**: By default, source data is deleted incrementally as it is migrated, but only after each chunk is durably written and validated in the destination
- **Parallel**: Migrate multiple databases concurrently with `--parallel`

### Databases migrated

The tool resolves each database's real location from your node configuration:

| Database              | Location                         | Backend setting  |
|-----------------------|----------------------------------|------------------|
| `application.db`      | `<home>/data`                    | `app-db-backend` |
| `snapshots/metadata.db` | `<home>/data/snapshots`        | `app-db-backend` |
| `blockstore.db`       | `cfg.DBDir()` (config `db_dir`)  | `db_backend`     |
| `state.db`            | `cfg.DBDir()` (config `db_dir`)  | `db_backend`     |
| `tx_index.db`         | `cfg.DBDir()` (config `db_dir`)  | `db_backend`     |
| `evidence.db`         | `cfg.DBDir()` (config `db_dir`)  | `db_backend`     |

> **Important — two backend settings.** The app layer reads `app-db-backend`
> from `app.toml` first, falling back to `db_backend` in `config.toml`. The
> consensus layer reads `db_backend`. The tool detects the *effective* backend
> using the same precedence and, on swap, updates **both** `app-db-backend`
> (in `app.toml`, if present) and `db_backend` (in `config.toml`) so the app and
> consensus layers agree.
>
> **Custom `db_dir`.** If your `config.toml` sets a custom `db_dir`, the four
> consensus databases live there (possibly on a different disk) and the tool
> migrates them in place. `application.db` and `snapshots/` always live under
> `<home>/data`.

Files NOT migrated (remain unchanged): `cs.wal`, `priv_validator_state.json`,
`traces/`, and the snapshot *chunk* files under `snapshots/` (only
`snapshots/metadata.db` is migrated).

## Installation

```bash
cd tools/migrate-db
go build -o migrate-db
```

## Usage

### Basic migration

```bash
./migrate-db
```

This will, for every database:

1. Copy it into a staging directory (`.pebble-migrate/`) **next to its source**
   (so the final swap is an atomic same-filesystem rename, even with a custom `db_dir`),
   deleting source data incrementally to save disk (default).
2. Reopen and verify the destination (consistency + full read-back).
3. Move the old database aside, move the new one into place, fsync, and re-verify in place.
4. Update `app-db-backend` (app.toml) and `db_backend` (config.toml) to `pebbledb`.

### Options

| Flag                   | Default           | Description                                            |
|------------------------|-------------------|--------------------------------------------------------|
| `--home <path>`        | `~/.celestia-app` | Node home directory                                    |
| `--dry-run`            | `false`           | Show what would be migrated without making changes     |
| `--backup`             | `false`           | Keep source LevelDB data after migration (preserved through swap) |
| `--batch-size <MB>`    | `64`              | Write batch size in MB                                 |
| `--delete-chunk <MB>`  | `1024`            | Delete source keys every N MB migrated (no-backup mode)|
| `--sync-interval <MB>` | `1024`            | Fsync the dest WAL every N MB (0 = sync at chunk/DB boundaries only) |
| `--parallel <N>`       | `3`               | Migrate N databases concurrently                       |
| `--db <name>`          | all               | Migrate only a specific database (disables auto-swap)  |
| `--manual-swap`        | `false`           | Skip auto-swap; print manual instructions instead      |
| `--skip-compact`       | `false`           | Skip post-migration PebbleDB compaction (not recommended)|
| `--check`              | `false`           | Don't migrate; open the existing databases and verify they are consistent |

### Examples

**Recommended (safest) — keep a backup until you've confirmed the node runs:**

```bash
./migrate-db --backup
```

This migrates and auto-swaps, but preserves the old LevelDB directories as
`<db>.db.leveldb-bak`. Once the node is confirmed healthy, delete them.

**Default (delete source incrementally):**

```bash
./migrate-db
```

**Dry-run:**

```bash
./migrate-db --dry-run
```

**Verify an already-migrated node opens consistently:**

```bash
./migrate-db --check
```

**Migrate a single database (no auto-swap):**

```bash
./migrate-db --db blockstore --manual-swap
```

## Resuming interrupted migrations

The tool is fully resumable. If interrupted for any reason, just re-run the same
command. It will:

1. Detect the existing `data_pebble/.migration_state.json` and per-database staging dirs
2. Skip databases that are already complete
3. For in-progress databases, find the last migrated key (verified against the source) and continue
4. Re-run verification before swapping/deleting

## Complete migration process

### 1. Stop your node

```bash
sudo systemctl stop celestia-appd
```

### 2. Check disk space

The migration plus the final compaction (and the node's first-start WAL replay)
can transiently require up to **~2× the size of the largest single database**.

```bash
du -sh ~/.celestia-app/data
df -h ~/.celestia-app
```

### 3. Run migration

```bash
cd /path/to/celestia-app/tools/migrate-db
go build -o migrate-db
./migrate-db --backup   # recommended the first time
```

### 4. Start and verify

Start the node **manually first** (or raise the systemd `TimeoutStartSec`) so a
slow first start isn't killed mid-way:

```bash
sudo systemctl start celestia-appd
celestia-appd status
journalctl -u celestia-appd -f
```

You can also run `./migrate-db --check` to confirm every database opens and
fully reads back.

### 5. Cleanup (after verification)

```bash
# If you used --backup:
rm -rf ~/.celestia-app/data/*.db.leveldb-bak
rm -rf ~/.celestia-app/customdb/*.db.leveldb-bak   # if a custom db_dir was used
```

## Crash recovery

| Scenario                          | What happens                                                          |
|-----------------------------------|-----------------------------------------------------------------------|
| Ctrl-C / kill during copy         | Uncommitted batch is lost. Re-run resumes from the last committed key. |
| Power loss during copy            | At most one delete-chunk of data is re-migrated; source keys are only deleted after the destination is durably flushed and validated. |
| Crash during swap                 | Swap is rolled back (old DB restored, new DB returned to staging). Re-run to retry. |
| Crash after config flip           | Re-run detects the migration state and resumes the swap/verify. |
| `data_pebble/` manually deleted   | Fresh start — no state to resume from.                                |

## Verification & durability

- During the copy, the destination is durably flushed (memtables → SSTs) and the
  WAL is fsynced before any source keys are deleted, and each deleted chunk is
  read back and compared against the source first.
- After the copy, the destination is **reopened** (which runs PebbleDB's
  `checkConsistency`) and **fully iterated** (reading every value) to surface any
  corruption or missing SST file.
- In `--backup` mode the full source and destination are compared by key count
  and content hash before the swap.

## Compaction

- **Source LevelDB**: after each `--delete-chunk` is deleted, the deleted key
  range is compacted so disk space is reclaimed promptly.
- **Target PebbleDB**: after all keys are copied (and flushed), a full
  compaction over the entire key range merges the many SST files created by bulk
  writes. Use `--skip-compact` to skip it (faster, larger result).
