package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	db "github.com/cosmos/cosmos-db"
	"github.com/gofrs/flock"
	"golang.org/x/sync/errgroup"
)

const (
	// deleteChunkBytes is the amount of data migrated before deleting source keys when backup is disabled.
	deleteChunkBytes = 1024 * 1024 * 1024 // 1 GB

	// maxDeleteBatch is the maximum size of a single delete batch written to the source DB.
	maxDeleteBatch = 64 * 1024 * 1024 // 64 MB

	// progressInterval is how often progress is logged during migration.
	progressInterval = 2 * time.Minute
)

var allDatabases = []string{
	"application",
	"blockstore",
	"state",
	"tx_index",
	"evidence",
}

// MigrationState tracks overall migration progress across restarts.
type MigrationState struct {
	StartedAt time.Time          `json:"started_at"`
	Backup    bool               `json:"backup"`
	Databases map[string]DBState `json:"databases"`
}

// stateTracker bundles MigrationState with a mutex for concurrent access and the dest dir for persistence.
type stateTracker struct {
	mu      sync.Mutex
	state   *MigrationState
	destDir string
}

// getDBState returns the current state of a database under lock.
func (st *stateTracker) getDBState(dbName string) DBState {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.state.Databases[dbName]
}

// updateDBState persists an updated database state to disk under lock.
func (st *stateTracker) updateDBState(dbName string, ds DBState) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.state.Databases[dbName] = ds
	return saveState(st.state, st.destDir)
}

type dbStatus string

const (
	statusPending       dbStatus = "pending"
	statusInProgress    dbStatus = "in_progress"
	statusMigrated      dbStatus = "migrated"
	statusSourceDeleted dbStatus = "source_deleted"
)

// DBState tracks the migration status of a single database.
type DBState struct {
	Status        dbStatus  `json:"status"`
	KeysMigrated  int64     `json:"keys_migrated"`
	BytesMigrated int64     `json:"bytes_migrated"`
	CompletedAt   time.Time `json:"completed_at"`
}

type migrateOpts struct {
	homeDir      string
	dryRun       bool
	backup       bool
	batchSizeMB  int
	syncInterval int
	parallel     int
	verify       bool
	dbFilter     string
	manualSwap   bool
}

func main() {
	opts := migrateOpts{}
	flag.StringVar(&opts.homeDir, "home", os.ExpandEnv("$HOME/.celestia-app"), "Node home directory")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "Run migration in dry-run mode without making changes")
	flag.BoolVar(&opts.backup, "backup", false, "Keep source LevelDB data after migration (default: delete incrementally)")
	flag.IntVar(&opts.batchSizeMB, "batch-size", 64, "Batch size in MB")
	flag.IntVar(&opts.syncInterval, "sync-interval", 1024, "Fsync every N MB (0 = sync only at DB end)")
	flag.IntVar(&opts.parallel, "parallel", 3, "Migrate N databases concurrently")
	flag.BoolVar(&opts.verify, "verify", false, "Run sample verification after migration")
	flag.StringVar(&opts.dbFilter, "db", "", "Migrate only a specific database (e.g. --db blockstore)")
	flag.BoolVar(&opts.manualSwap, "manual-swap", false, "Skip auto-swap; print manual instructions instead")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: migrate-db [options]

Migrate celestia-app databases from LevelDB to PebbleDB.

This tool is resumable and idempotent. If interrupted, simply re-run
to continue from where it left off. On resume, the last written key
is verified against the source before continuing.

By default, source data is deleted incrementally as it is migrated
and databases are auto-swapped into place after migration.

Databases migrated:
- application.db, blockstore.db, state.db, tx_index.db, evidence.db

Options:
`)
		flag.PrintDefaults()
	}

	flag.Parse()

	if opts.parallel < 1 {
		opts.parallel = 1
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	if err := runMigration(ctx, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		cancel()
		os.Exit(1)
	}
	cancel()
}

// runMigration orchestrates the full LevelDB-to-PebbleDB migration.
func runMigration(ctx context.Context, opts migrateOpts) error {
	dataDir := filepath.Join(opts.homeDir, "data")
	pebbleDataDir := filepath.Join(opts.homeDir, "data_pebble")

	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		return fmt.Errorf("data directory does not exist: %s", dataDir)
	}

	databases := allDatabases
	if opts.dbFilter != "" {
		if !slices.Contains(allDatabases, opts.dbFilter) {
			return fmt.Errorf("unknown database %q, valid options: %s", opts.dbFilter, strings.Join(allDatabases, ", "))
		}
		databases = []string{opts.dbFilter}
	}

	printBanner(opts, dataDir, pebbleDataDir)

	if opts.dryRun {
		for _, dbName := range databases {
			levelDBPath := filepath.Join(dataDir, dbName+".db")
			if _, err := os.Stat(levelDBPath); os.IsNotExist(err) {
				fmt.Printf("[%s] Warning: LevelDB not found, would skip\n", dbName)
				continue
			}
			fmt.Printf("[%s] Would migrate %s -> %s/%s.db\n", dbName, levelDBPath, pebbleDataDir, dbName)
		}
		fmt.Println("\nDry-run complete. No changes were made.")
		return nil
	}

	// Create dest directory (ok if it already exists — resume case)
	if err := os.MkdirAll(pebbleDataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create pebble data directory: %w", err)
	}

	// Acquire kernel-level file lock (automatically released on crash)
	lockPath := filepath.Join(pebbleDataDir, ".migration.lock")
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryLock()
	if err != nil {
		return fmt.Errorf("failed to acquire lock %s: %w", lockPath, err)
	}
	if !locked {
		return fmt.Errorf("another migration is running (lock held on %s)", lockPath)
	}
	defer func() {
		if err := fileLock.Unlock(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to release migration lock: %v\n", err)
		}
	}()

	state, err := loadOrInitState(pebbleDataDir, opts)
	if err != nil {
		return err
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.parallel)
	for _, dbName := range databases {
		g.Go(func() error {
			return migrateOneDB(gctx, dbName, dataDir, state, opts)
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	if !opts.manualSwap {
		return performAutoSwap(opts.homeDir, dataDir, pebbleDataDir, opts.backup)
	}

	printNextSteps(dataDir, pebbleDataDir, opts.backup)
	return nil
}

// printBanner prints the migration configuration summary.
func printBanner(opts migrateOpts, dataDir, pebbleDataDir string) {
	fmt.Printf("Starting database migration from LevelDB to PebbleDB\n")
	fmt.Printf("Home directory:    %s\n", opts.homeDir)
	fmt.Printf("Source (LevelDB):  %s\n", dataDir)
	fmt.Printf("Dest (PebbleDB):   %s\n", pebbleDataDir)
	fmt.Printf("Dry-run:           %v\n", opts.dryRun)
	fmt.Printf("Backup:            %v\n", opts.backup)
	fmt.Printf("Batch size:        %d MB\n", opts.batchSizeMB)
	fmt.Printf("Sync interval:     %d MB\n", opts.syncInterval)
	fmt.Printf("Parallel:          %d\n", opts.parallel)
	fmt.Println()
}

// loadOrInitState loads existing migration state or creates a fresh one.
func loadOrInitState(pebbleDataDir string, opts migrateOpts) (*stateTracker, error) {
	state, err := loadState(pebbleDataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load migration state: %w", err)
	}

	if state == nil {
		state = &MigrationState{
			StartedAt: time.Now(),
			Backup:    opts.backup,
			Databases: make(map[string]DBState),
		}
		for _, d := range allDatabases {
			state.Databases[d] = DBState{Status: statusPending}
		}
		if err := saveState(state, pebbleDataDir); err != nil {
			return nil, err
		}
		fmt.Println("Initialized new migration state.")
	} else {
		fmt.Printf("Resuming migration started at %s\n", state.StartedAt.Format(time.RFC3339))
		for name, ds := range state.Databases {
			if ds.Status != statusPending {
				fmt.Printf("  [%s] status=%s keys=%d bytes=%s\n", name, ds.Status, ds.KeysMigrated, humanBytes(ds.BytesMigrated))
			}
		}
		fmt.Println()
	}

	return &stateTracker{state: state, destDir: pebbleDataDir}, nil
}

// migrateOneDB handles a single database end-to-end: state transitions, copy, verify, and cleanup.
func migrateOneDB(ctx context.Context, dbName, dataDir string, tracker *stateTracker, opts migrateOpts) error {
	ds := tracker.getDBState(dbName)

	if ds.Status == statusMigrated || ds.Status == statusSourceDeleted {
		fmt.Printf("[%s] Already complete (status=%s), skipping\n", dbName, ds.Status)
		return nil
	}

	levelDBPath := filepath.Join(dataDir, dbName+".db")
	if _, err := os.Stat(levelDBPath); os.IsNotExist(err) {
		if ds.Status == statusInProgress {
			// Source was deleted (no-backup crash recovery), but dest should have data
			fmt.Printf("[%s] Source not found but was in_progress — marking as migrated\n", dbName)
			ds.Status = statusMigrated
			ds.CompletedAt = time.Now()
			return tracker.updateDBState(dbName, ds)
		}
		fmt.Printf("[%s] Warning: LevelDB not found, skipping\n", dbName)
		return nil
	}

	ds.Status = statusInProgress
	if err := tracker.updateDBState(dbName, ds); err != nil {
		return fmt.Errorf("[%s] failed to save state: %w", dbName, err)
	}

	fmt.Printf("[%s] Starting migration...\n", dbName)
	keys, bytesMigrated, err := migrateSingleDB(ctx, dbName, dataDir, tracker.destDir, opts)
	if err != nil {
		return fmt.Errorf("[%s] migration failed: %w", dbName, err)
	}

	ds.Status = statusMigrated
	ds.KeysMigrated = keys
	ds.BytesMigrated = bytesMigrated
	ds.CompletedAt = time.Now()
	if err := tracker.updateDBState(dbName, ds); err != nil {
		return fmt.Errorf("[%s] failed to save state: %w", dbName, err)
	}

	if opts.verify {
		fmt.Printf("[%s] Verifying...\n", dbName)
		if err := verifyDBSample(dbName, dataDir, tracker.destDir, 1000); err != nil {
			return fmt.Errorf("[%s] sample verification failed: %w", dbName, err)
		}
		fmt.Printf("[%s] Verification passed\n", dbName)
	}

	// Delete source unless --backup
	if !opts.backup {
		srcPath := filepath.Join(dataDir, dbName+".db")
		fmt.Printf("[%s] Removing source LevelDB: %s\n", dbName, srcPath)
		if err := os.RemoveAll(srcPath); err != nil {
			return fmt.Errorf("[%s] failed to remove source: %w", dbName, err)
		}
		ds.Status = statusSourceDeleted
		if err := tracker.updateDBState(dbName, ds); err != nil {
			return fmt.Errorf("[%s] failed to save state: %w", dbName, err)
		}
	}

	fmt.Printf("[%s] Complete: %d keys, %s\n", dbName, keys, humanBytes(bytesMigrated))
	return nil
}

// migrateSingleDB opens source and dest DBs, finds the resume point, and dispatches to the appropriate copy function.
func migrateSingleDB(ctx context.Context, dbName, sourceDir, destDir string, opts migrateOpts) (int64, int64, error) {
	startTime := time.Now()
	batchBytes := int64(opts.batchSizeMB) * 1024 * 1024
	syncBytes := int64(opts.syncInterval) * 1024 * 1024

	// Open source LevelDB
	sourceDB, err := db.NewDB(dbName, db.GoLevelDBBackend, sourceDir)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open source LevelDB: %w", err)
	}
	defer func() { _ = sourceDB.Close() }()

	// Open destination PebbleDB (creates if not exists, opens if exists)
	destDB, err := db.NewDB(dbName, db.PebbleDBBackend, destDir)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open destination PebbleDB: %w", err)
	}
	defer func() { _ = destDB.Close() }()

	resumeKey, err := findResumePoint(destDB, dbName)
	if err != nil {
		return 0, 0, err
	}

	if err := verifyResumePoint(sourceDB, destDB, resumeKey, dbName); err != nil {
		return 0, 0, err
	}

	srcIter, err := iteratorFrom(sourceDB, resumeKey)
	if err != nil {
		return 0, 0, err
	}

	var totalKeys, totalBytes int64
	if !opts.backup {
		totalKeys, totalBytes, err = copyAndDeleteKeys(ctx, dbName, sourceDB, destDB, srcIter, batchBytes, syncBytes, startTime)
	} else {
		totalKeys, totalBytes, err = copyAllKeys(ctx, dbName, destDB, srcIter, batchBytes, syncBytes, startTime)
	}
	if err != nil {
		return 0, 0, err
	}

	elapsed := time.Since(startTime)
	fmt.Printf("[%s] Migration complete: %d keys copied, %s, elapsed %s\n",
		dbName, totalKeys, humanBytes(totalBytes), elapsed.Round(time.Second))

	return totalKeys, totalBytes, nil
}

// findResumePoint returns the last key in destDB, or nil if the DB is empty.
func findResumePoint(destDB db.DB, dbName string) ([]byte, error) {
	revIter, err := destDB.ReverseIterator(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create reverse iterator: %w", err)
	}
	var resumeKey []byte
	if revIter.Valid() {
		resumeKey = make([]byte, len(revIter.Key()))
		copy(resumeKey, revIter.Key())
		fmt.Printf("[%s] Resuming from previous run\n", dbName)
	}
	err = revIter.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close reverse iterator: %w", err)
	}
	return resumeKey, nil
}

// verifyResumePoint checks that the last key written to destDB matches the source value.
// If the key was already deleted from source (no-backup mode), the check is skipped.
func verifyResumePoint(sourceDB, destDB db.DB, resumeKey []byte, dbName string) error {
	if resumeKey == nil {
		return nil
	}
	srcVal, err := sourceDB.Get(resumeKey)
	if err != nil {
		return fmt.Errorf("failed to read resume key from source: %w", err)
	}
	if srcVal == nil {
		// Source key was deleted (no-backup mode) — can't verify
		fmt.Printf("[%s] Resume key not in source (already deleted), skipping resume verification\n", dbName)
		return nil
	}
	destVal, err := destDB.Get(resumeKey)
	if err != nil {
		return fmt.Errorf("failed to read resume key from dest: %w", err)
	}
	if !bytes.Equal(srcVal, destVal) {
		return fmt.Errorf("[%s] resume verification failed: value mismatch at last written key", dbName)
	}
	fmt.Printf("[%s] Resume point verified\n", dbName)
	return nil
}

// iteratorFrom creates a source iterator positioned after resumeKey, or from the start if nil.
func iteratorFrom(sourceDB db.DB, resumeKey []byte) (db.Iterator, error) {
	if resumeKey != nil {
		srcIter, err := sourceDB.Iterator(resumeKey, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create source iterator: %w", err)
		}
		// Skip the resume key itself if it matches (already migrated)
		if srcIter.Valid() && bytes.Equal(srcIter.Key(), resumeKey) {
			srcIter.Next()
		}
		return srcIter, nil
	}
	srcIter, err := sourceDB.Iterator(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create source iterator: %w", err)
	}
	return srcIter, nil
}

// flushBatch writes the batch (sync or async), closes it, and returns a fresh batch.
func flushBatch(batch db.Batch, destDB db.DB, sync bool) (db.Batch, error) {
	var writeErr error
	if sync {
		writeErr = batch.WriteSync()
	} else {
		writeErr = batch.Write()
	}
	if writeErr != nil {
		_ = batch.Close()
		return nil, fmt.Errorf("failed to write batch: %w", writeErr)
	}
	err := batch.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close batch: %w", err)
	}
	return destDB.NewBatch(), nil
}

// logProgress logs migration throughput at progressInterval.
func logProgress(dbName string, totalKeys, totalBytes int64, startTime time.Time, lastLogTime *time.Time) {
	if time.Since(*lastLogTime) >= progressInterval {
		elapsed := time.Since(startTime)
		rate := float64(totalBytes) / elapsed.Seconds()
		fmt.Printf("[%s] %d keys, %s migrated, %s/s, elapsed %s\n",
			dbName, totalKeys, humanBytes(totalBytes), humanBytes(int64(rate)),
			elapsed.Round(time.Second))
		*lastLogTime = time.Now()
	}
}

// copyAllKeys copies all keys from srcIter into destDB in batches. Returns total keys and bytes copied.
func copyAllKeys(ctx context.Context, dbName string, destDB db.DB, srcIter db.Iterator, batchBytes, syncBytes int64, startTime time.Time) (int64, int64, error) {
	var totalKeys int64
	var totalBytes, bytesSinceSync int64
	lastLogTime := time.Now()

	batch := destDB.NewBatch()
	var batchKeyCount int

	for ; srcIter.Valid(); srcIter.Next() {
		key := srcIter.Key()
		value := srcIter.Value()
		kvSize := int64(len(key) + len(value))

		if err := batch.Set(key, value); err != nil {
			_ = srcIter.Close()
			_ = batch.Close()
			return 0, 0, fmt.Errorf("failed to set key in batch: %w", err)
		}

		totalKeys++
		batchKeyCount++
		totalBytes += kvSize
		bytesSinceSync += kvSize

		currentBatchSize, _ := batch.GetByteSize()
		if int64(currentBatchSize) >= batchBytes {
			needSync := syncBytes > 0 && bytesSinceSync >= syncBytes
			var err error
			batch, err = flushBatch(batch, destDB, needSync)
			if err != nil {
				_ = srcIter.Close()
				return 0, 0, err
			}
			batchKeyCount = 0
			if needSync {
				bytesSinceSync = 0
			}

			// check whether the context was canceled
			if err := ctx.Err(); err != nil {
				_ = srcIter.Close()
				return 0, 0, fmt.Errorf("cancelled: %w", err)
			}
		}

		logProgress(dbName, totalKeys, totalBytes, startTime, &lastLogTime)
	}

	if err := srcIter.Error(); err != nil {
		_ = srcIter.Close()
		_ = batch.Close()
		return 0, 0, fmt.Errorf("iterator error: %w", err)
	}
	err := srcIter.Close()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to close source iterator: %w", err)
	}

	if batchKeyCount > 0 {
		if err := batch.WriteSync(); err != nil {
			_ = batch.Close()
			return 0, 0, fmt.Errorf("failed to write batch: %w", err)
		}
	}
	_ = batch.Close()

	return totalKeys, totalBytes, nil
}

// copyAndDeleteKeys copies keys into destDB and incrementally deletes them from sourceDB.
func copyAndDeleteKeys(ctx context.Context, dbName string, sourceDB, destDB db.DB, srcIter db.Iterator, batchBytes, syncBytes int64, startTime time.Time) (int64, int64, error) {
	var totalKeys int64
	var totalBytes, bytesSinceSync, bytesSinceDelete int64
	var deleteKeys [][]byte
	lastLogTime := time.Now()

	batch := destDB.NewBatch()
	var batchKeyCount int

	for ; srcIter.Valid(); srcIter.Next() {
		key := srcIter.Key()
		value := srcIter.Value()
		kvSize := int64(len(key) + len(value))

		if err := batch.Set(key, value); err != nil {
			_ = srcIter.Close()
			_ = batch.Close()
			return 0, 0, fmt.Errorf("failed to set key in batch: %w", err)
		}

		totalKeys++
		batchKeyCount++
		totalBytes += kvSize
		bytesSinceSync += kvSize
		bytesSinceDelete += kvSize

		keyCopy := make([]byte, len(key))
		copy(keyCopy, key)
		deleteKeys = append(deleteKeys, keyCopy)

		currentBatchSize, _ := batch.GetByteSize()
		if int64(currentBatchSize) >= batchBytes {
			needSync := syncBytes > 0 && bytesSinceSync >= syncBytes
			var err error
			batch, err = flushBatch(batch, destDB, needSync)
			if err != nil {
				_ = srcIter.Close()
				return 0, 0, err
			}
			batchKeyCount = 0
			if needSync {
				bytesSinceSync = 0
			}

			if err := ctx.Err(); err != nil {
				_ = srcIter.Close()
				return 0, 0, fmt.Errorf("cancelled: %w", err)
			}
		}

		logProgress(dbName, totalKeys, totalBytes, startTime, &lastLogTime)

		// Incremental source deletion every ~1GB
		if bytesSinceDelete >= deleteChunkBytes {
			// Flush any pending batch first (with sync for durability before delete)
			if batchKeyCount > 0 {
				var err error
				batch, err = flushBatch(batch, destDB, true)
				if err != nil {
					_ = srcIter.Close()
					return 0, 0, err
				}
				batchKeyCount = 0
				bytesSinceSync = 0
			}

			// Close source iterator before deleting
			lastKey := make([]byte, len(key))
			copy(lastKey, key)
			if err := srcIter.Close(); err != nil {
				return 0, 0, fmt.Errorf("failed to close source iterator: %w", err)
			}

			if err := deleteSourceKeys(sourceDB, deleteKeys); err != nil {
				return 0, 0, fmt.Errorf("failed to delete source keys: %w", err)
			}
			deleteKeys = deleteKeys[:0]
			bytesSinceDelete = 0

			// Reopen source iterator from last position
			var err error
			srcIter, err = sourceDB.Iterator(lastKey, nil)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to reopen source iterator: %w", err)
			}
			// Skip lastKey if it still exists (might have been deleted above)
			if srcIter.Valid() && bytes.Equal(srcIter.Key(), lastKey) {
				srcIter.Next()
			}
		}
	}

	if err := srcIter.Error(); err != nil {
		_ = srcIter.Close()
		_ = batch.Close()
		return 0, 0, fmt.Errorf("iterator error: %w", err)
	}
	if err := srcIter.Close(); err != nil {
		return 0, 0, fmt.Errorf("failed to close source iterator: %w", err)
	}

	if batchKeyCount > 0 {
		if err := batch.WriteSync(); err != nil {
			_ = batch.Close()
			return 0, 0, fmt.Errorf("failed to write batch: %w", err)
		}
	}
	_ = batch.Close()

	// Delete any remaining tracked keys
	if len(deleteKeys) > 0 {
		if err := deleteSourceKeys(sourceDB, deleteKeys); err != nil {
			return 0, 0, fmt.Errorf("failed to delete remaining source keys: %w", err)
		}
	}

	return totalKeys, totalBytes, nil
}

// deleteSourceKeys deletes the given keys from sourceDB in batches capped at maxDeleteBatch.
func deleteSourceKeys(sourceDB db.DB, keys [][]byte) error {
	batch := sourceDB.NewBatch()
	for _, key := range keys {
		if err := batch.Delete(key); err != nil {
			_ = batch.Close()
			return err
		}
		size, _ := batch.GetByteSize()
		if size >= maxDeleteBatch {
			if err := batch.WriteSync(); err != nil {
				_ = batch.Close()
				return err
			}
			_ = batch.Close()
			batch = sourceDB.NewBatch()
		}
	}
	if err := batch.WriteSync(); err != nil {
		_ = batch.Close()
		return err
	}
	return batch.Close()
}

// verifyDBSample picks evenly-spaced keys from source and verifies they exist with same value in dest.
func verifyDBSample(dbName, sourceDir, destDir string, sampleSize int) error {
	sourceDB, err := db.NewDB(dbName, db.GoLevelDBBackend, sourceDir)
	if err != nil {
		return fmt.Errorf("failed to open source for verification: %w", err)
	}
	defer func() { _ = sourceDB.Close() }()

	destDB, err := db.NewDB(dbName, db.PebbleDBBackend, destDir)
	if err != nil {
		return fmt.Errorf("failed to open dest for verification: %w", err)
	}
	defer func() { _ = destDB.Close() }()

	// First pass: count total keys
	var totalKeys int64
	countIter, err := sourceDB.Iterator(nil, nil)
	if err != nil {
		return err
	}
	for ; countIter.Valid(); countIter.Next() {
		totalKeys++
	}
	_ = countIter.Close()

	if totalKeys == 0 {
		fmt.Printf("[%s] Source is empty, nothing to verify\n", dbName)
		return nil
	}

	stride := max(totalKeys/int64(sampleSize), 1)

	// Second pass: sample and verify
	iter, err := sourceDB.Iterator(nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = iter.Close() }()

	var checked, mismatches int64
	var idx int64
	for ; iter.Valid(); iter.Next() {
		if idx%stride == 0 {
			key := iter.Key()
			srcVal := iter.Value()
			destVal, err := destDB.Get(key)
			if err != nil {
				return fmt.Errorf("dest Get failed for key: %w", err)
			}
			if !bytes.Equal(srcVal, destVal) {
				mismatches++
				if mismatches <= 3 {
					fmt.Printf("[%s] Mismatch at key %x\n", dbName, key)
				}
			}
			checked++
		}
		idx++
	}

	if mismatches > 0 {
		return fmt.Errorf("sample verification found %d mismatches out of %d checked", mismatches, checked)
	}
	fmt.Printf("[%s] Sample verification passed: %d/%d keys checked\n", dbName, checked, totalKeys)
	return nil
}

// State file management

// loadState reads the migration state file, returning nil if it doesn't exist.
func loadState(destDir string) (*MigrationState, error) {
	path := filepath.Join(destDir, ".migration_state.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state MigrationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("corrupt migration state file: %w", err)
	}
	return &state, nil
}

// saveState atomically writes the migration state file via tmp+rename.
func saveState(state *MigrationState, destDir string) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := filepath.Join(destDir, ".migration_state.json.tmp")
	finalPath := filepath.Join(destDir, ".migration_state.json")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, finalPath)
}

// performAutoSwap moves PebbleDB files into data/ and updates config.toml.
func performAutoSwap(homeDir, dataDir, pebbleDataDir string, backup bool) error {
	fmt.Println("\nPerforming auto-swap...")

	for _, dbName := range allDatabases {
		srcPath := filepath.Join(pebbleDataDir, dbName+".db")
		dstPath := filepath.Join(dataDir, dbName+".db")

		// Remove old LevelDB if it still exists
		if backup {
			if _, err := os.Stat(dstPath); err == nil {
				fmt.Printf("  Removing old %s\n", dstPath)
				if err := os.RemoveAll(dstPath); err != nil {
					return fmt.Errorf("failed to remove old %s: %w", dstPath, err)
				}
			}
		}

		// Move PebbleDB into place
		if _, err := os.Stat(srcPath); err == nil {
			fmt.Printf("  Moving %s -> %s\n", srcPath, dstPath)
			if err := os.Rename(srcPath, dstPath); err != nil {
				return fmt.Errorf("failed to move %s: %w", dbName, err)
			}
		}
	}

	// Update config.toml
	configPath := filepath.Join(homeDir, "config", "config.toml")
	if err := updateConfigBackend(configPath, "pebbledb"); err != nil {
		fmt.Printf("  Warning: could not update config.toml: %v\n", err)
		fmt.Printf("  Please manually set db_backend = \"pebbledb\" in %s\n", configPath)
	} else {
		fmt.Printf("  Updated %s: db_backend = \"pebbledb\"\n", configPath)
	}

	// Clean up
	_ = os.Remove(filepath.Join(pebbleDataDir, ".migration_state.json"))
	_ = os.Remove(filepath.Join(pebbleDataDir, ".migration.lock"))
	_ = os.Remove(pebbleDataDir)

	fmt.Println("\nAuto-swap complete. Start your node to verify.")
	return nil
}

// updateConfigBackend rewrites the db_backend line in config.toml.
func updateConfigBackend(configPath, backend string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "db_backend") && strings.Contains(trimmed, "=") {
			lines[i] = fmt.Sprintf(`db_backend = "%s"`, backend)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("db_backend setting not found in config.toml")
	}
	return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0o644)
}

// printNextSteps prints post-migration instructions to stdout.
func printNextSteps(dataDir, pebbleDataDir string, backup bool) {
	var rmCommands, mvCommands strings.Builder
	for _, dbName := range allDatabases {
		if backup {
			fmt.Fprintf(&rmCommands, "   rm -rf %s/%s.db\n", dataDir, dbName)
		}
		fmt.Fprintf(&mvCommands, "   mv %s/%s.db %s/%s.db\n", pebbleDataDir, dbName, dataDir, dbName)
	}

	fmt.Printf(`
Migration completed successfully!

============================================================
Next Steps:
============================================================

1. Update config.toml to use PebbleDB:
   db_backend = "pebbledb"

2. Move the migrated databases:
`)
	if backup {
		fmt.Printf("   # Remove old databases\n%s\n", rmCommands.String())
	}
	fmt.Printf("   # Move PebbleDB files\n%s", mvCommands.String())
	fmt.Printf(`
3. Start your node and verify that it is running properly

4. Cleanup (after verification):
   rm -rf %s

============================================================
`, pebbleDataDir)
}

// humanBytes formats a byte count as a human-readable string (e.g. "1.50 GB").
func humanBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
		tb = 1024 * gb
	)
	switch {
	case b >= tb:
		return fmt.Sprintf("%.2f TB", float64(b)/float64(tb))
	case b >= gb:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
