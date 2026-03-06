# Implementation Plan: Resumable, Idempotent, TB-Scale migrate-db Tool

## Current State

The `tools/migrate-db/main.go` implements a production-ready LevelDB-to-PebbleDB migration tool with the following capabilities:

- Resumable via reverse iterator on destination PebbleDB (no separate checkpoint file)
- Idempotent — re-running after completion is a no-op
- Byte-based batching (64MB default) with configurable fsync intervals
- Incremental source deletion in `--no-backup` mode (~1GB chunks)
- Parallel database migration via `golang.org/x/sync/errgroup`
- Kernel-level file locking via `gofrs/flock` (auto-releases on crash)
- Atomic state file writes (tmp + rename)
- Sampling and full verification modes
- Auto-swap of PebbleDB files into `data/` with `config.toml` update

## Architecture

### Resume via Reverse Iterator

The last key in the destination PebbleDB IS the durable checkpoint. On resume:

1. Open dest PebbleDB
2. `ReverseIterator(nil, nil)` -> get `lastKey`
3. Open source `Iterator(lastKey, nil)` -> skip first entry if it equals `lastKey`
4. Continue migrating from there

No separate checkpoint file needed for byte-level progress.

### State File (`data_pebble/.migration_state.json`)

Tracks which of the 5 databases are complete:

```go
type MigrationState struct {
    StartedAt time.Time          `json:"started_at"`
    NoBackup  bool               `json:"no_backup"`
    Databases map[string]DBState `json:"databases"`
}

type DBState struct {
    Status        string    `json:"status"` // "pending" | "in_progress" | "migrated" | "source_deleted"
    KeysMigrated  int64     `json:"keys_migrated"`
    BytesMigrated int64     `json:"bytes_migrated"`
    CompletedAt   time.Time `json:"completed_at,omitempty"`
}
```

Written atomically via write-to-tmp + `os.Rename`.

### Incremental Source Deletion (`--no-backup`)

1. Migrate batches (64MB each) to PebbleDB via `Write()` (async)
2. After ~1GB migrated: close source iterator, delete accumulated keys via batched `WriteSync()`, reopen iterator
3. Closing/reopening lets LevelDB compact and reclaim space

On crash recovery: deleted source keys are already in PebbleDB. `Iterator(lastKey, nil)` skips past them.

### Write Durability (`--sync-interval`)

- Default 1024MB: async `Write()` per batch, `WriteSync()` every ~1GB
- 64MB: fsync every batch (slower, minimal re-work on crash)
- 0: fsync only at end of each database (fastest)

### Parallel Migration (`--parallel`)

Uses `errgroup.WithContext()` + `SetLimit(N)`. Each database is independent. State file writes are mutex-protected. First error cancels all goroutines via context.

### File Lock (`gofrs/flock`)

Kernel-level file lock on `data_pebble/.migration.lock`. Automatically released on crash — no stale lock detection needed.

## Package-Level Constants

```go
const (
    deleteChunkBytes = 1 << 30          // 1 GB — source deletion granularity
    maxDeleteBatch   = 64 * 1024 * 1024 // 64 MB — max single delete batch
    progressInterval = 2 * time.Minute  // progress log frequency
)
```

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--home <path>` | `~/.celestia-app` | Node home directory |
| `--dry-run` | `false` | Test without making changes |
| `--no-backup` | `false` | Delete source data incrementally as migrated |
| `--batch-size <MB>` | `64` | Write batch size in MB |
| `--sync-interval <MB>` | `1024` | Fsync every N MB (0 = sync only at DB end) |
| `--parallel <N>` | `1` | Migrate N databases concurrently |
| `--verify-full` | `false` | Exhaustive key-count verification |
| `--skip-verify` | `false` | Skip post-migration verification |
| `--db <name>` | all | Migrate only a specific database |
| `--auto-swap` | `false` | Move PebbleDB into `data/` and update `config.toml` |

## Key Functions

| Function | Purpose |
|----------|---------|
| `main()` | Parse flags, set up signal handling, call `migrateDB` |
| `migrateDB()` | Orchestration: lock, state, parallel/sequential dispatch, auto-swap |
| `migrateSingleDB()` | Core loop: resume, byte-batch, sync, incremental delete |
| `deleteSourceKeys()` | Batched deletion of source keys (64MB sub-batches) |
| `verifyDBSample()` | Sample 1000 evenly-spaced keys, compare source vs dest |
| `verifyDBFull()` | Count all dest keys, compare to expected |
| `loadState()` / `saveState()` | Atomic JSON state file management |
| `performAutoSwap()` | Move PebbleDB dirs, update `config.toml`, clean up |
| `updateConfigBackend()` | Parse and rewrite `db_backend` in `config.toml` |

## Crash Recovery Matrix

| Scenario | Recovery |
|---|---|
| Ctrl-C mid-batch (before Write) | Batch lost. Resume finds last durable key. Re-migrates lost keys. |
| Power loss (async writes not fsynced) | Up to `--sync-interval` MB re-migrated. Resume finds last fsynced key. |
| Crash after dest write, before source delete | Source keys still exist. Resume skips them (already in dest). |
| Crash after source delete, before state update | State says "in_progress". Resume opens dest, finds last key, continues. |
| Crash during state file write | Atomic rename ensures either old or new state. Reverse-iterator is real checkpoint. |
| `data_pebble/` manually deleted | Fresh start — no dest DB, no state file. |
| Crash while lock held | Kernel automatically releases flock — no stale lock possible. |
| Source deleted but migration incomplete | Detected on resume: source missing + status "in_progress" -> marked as migrated. |

## Error Handling Invariants

- All `saveState()` calls propagate errors — a failed state write stops migration immediately
- Batch write failures close the batch and return the error
- Iterator errors are checked after loop completion
- Context cancellation is checked once per batch flush via `select { case <-ctx.Done() }`

## Verification

1. `go build ./tools/migrate-db/` — compiles cleanly
2. `go vet ./tools/migrate-db/` — no issues
3. Fresh migration: run against test DB, verify all keys migrated
4. Resume: start, kill mid-way, restart — continues from checkpoint
5. `--no-backup`: source keys deleted incrementally, space reclaimed
6. Idempotency: run completed migration again — skips all DBs
7. `--db`: migrate single database, others untouched
8. `--parallel`: all DBs migrated correctly with concurrent output
9. `--auto-swap`: PebbleDB files in `data/`, `config.toml` updated
10. Lock: second instance fails with lock error
