package main

import (
	"bytes"
	"context"
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

	"github.com/celestiaorg/celestia-app/v8/app/migrate"
	db "github.com/cosmos/cosmos-db"
	"github.com/gofrs/flock"
	"golang.org/x/sync/errgroup"
)

const (
	// deleteChunkBytes is the amount of data migrated before deleting source keys in --no-backup mode.
	deleteChunkBytes = 1024 * 1024 * 1024 // 1 GB
)

type migrateOpts struct {
	homeDir      string
	dryRun       bool
	noBackup     bool
	batchSizeMB  int
	syncInterval int
	parallel     int
	verifyFull   bool
	skipVerify   bool
	dbFilter     string
	autoSwap     bool
}

func main() {
	opts := migrateOpts{}
	flag.StringVar(&opts.homeDir, "home", os.ExpandEnv("$HOME/.celestia-app"), "Node home directory")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "Run migration in dry-run mode without making changes")
	flag.BoolVar(&opts.noBackup, "no-backup", false, "Skip backup; delete source data incrementally as it is migrated")
	flag.IntVar(&opts.batchSizeMB, "batch-size", 256, "Batch size in MB")
	flag.IntVar(&opts.syncInterval, "sync-interval", 1024, "Fsync every N MB (0 = sync only at DB end)")
	flag.IntVar(&opts.parallel, "parallel", 3, "Migrate N databases concurrently")
	flag.BoolVar(&opts.verifyFull, "verify-full", false, "Exhaustive key-count verification instead of sampling")
	flag.BoolVar(&opts.skipVerify, "skip-verify", false, "Skip post-migration verification")
	flag.StringVar(&opts.dbFilter, "db", "", "Migrate only a specific database (e.g. --db blockstore)")
	flag.BoolVar(&opts.autoSwap, "auto-swap", false, "After migration, move PebbleDB files into data/ and update config.toml")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: migrate-db [options]

Migrate celestia-app databases from LevelDB to PebbleDB.

This tool is resumable and idempotent. If interrupted, simply re-run
to continue from where it left off.

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
	defer cancel()

	if err := migrateDB(ctx, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}

func migrateDB(ctx context.Context, opts migrateOpts) error {
	dataDir := filepath.Join(opts.homeDir, "data")
	pebbleDataDir := filepath.Join(opts.homeDir, "data_pebble")

	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		return fmt.Errorf("data directory does not exist: %s", dataDir)
	}

	databases := migrate.AllDatabases
	if opts.dbFilter != "" {
		if !slices.Contains(migrate.AllDatabases, opts.dbFilter) {
			return fmt.Errorf("unknown database %q, valid options: %s", opts.dbFilter, strings.Join(migrate.AllDatabases, ", "))
		}
		databases = []string{opts.dbFilter}
	}

	fmt.Printf("Starting database migration from LevelDB to PebbleDB\n")
	fmt.Printf("Home directory:    %s\n", opts.homeDir)
	fmt.Printf("Source (LevelDB):  %s\n", dataDir)
	fmt.Printf("Dest (PebbleDB):   %s\n", pebbleDataDir)
	fmt.Printf("Dry-run:           %v\n", opts.dryRun)
	fmt.Printf("No-backup:         %v\n", opts.noBackup)
	fmt.Printf("Batch size:        %d MB\n", opts.batchSizeMB)
	fmt.Printf("Sync interval:     %d MB\n", opts.syncInterval)
	fmt.Printf("Parallel:          %d\n", opts.parallel)
	fmt.Println()

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

	if err := os.MkdirAll(pebbleDataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create pebble data directory: %w", err)
	}

	lockPath := filepath.Join(pebbleDataDir, ".migration.lock")
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryLock()
	if err != nil {
		return fmt.Errorf("failed to acquire lock %s: %w", lockPath, err)
	}
	if !locked {
		return fmt.Errorf("another migration is running (lock held on %s)", lockPath)
	}
	defer fileLock.Unlock()

	state, err := migrate.LoadState(pebbleDataDir)
	if err != nil {
		return fmt.Errorf("failed to load migration state: %w", err)
	}

	if state == nil {
		state = &migrate.MigrationState{
			StartedAt: time.Now(),
			NoBackup:  opts.noBackup,
			Databases: make(map[string]migrate.DBState),
		}
		for _, d := range migrate.AllDatabases {
			state.Databases[d] = migrate.DBState{Status: "pending"}
		}
		if err := migrate.SaveState(state, pebbleDataDir); err != nil {
			return err
		}
		fmt.Println("Initialized new migration state.")
	} else {
		fmt.Printf("Resuming migration started at %s\n", state.StartedAt.Format(time.RFC3339))
		for name, ds := range state.Databases {
			if ds.Status != "pending" {
				fmt.Printf("  [%s] status=%s keys=%d bytes=%s\n", name, ds.Status, ds.KeysMigrated, migrate.HumanBytes(ds.BytesMigrated))
			}
		}
		fmt.Println()
	}

	var stateMu sync.Mutex

	migrateOne := func(ctx context.Context, dbName string) error {
		stateMu.Lock()
		ds := state.Databases[dbName]
		stateMu.Unlock()

		if ds.Status == "migrated" || ds.Status == "source_deleted" {
			fmt.Printf("[%s] Already complete (status=%s), skipping\n", dbName, ds.Status)
			return nil
		}

		levelDBPath := filepath.Join(dataDir, dbName+".db")
		if _, err := os.Stat(levelDBPath); os.IsNotExist(err) {
			if ds.Status == "in_progress" {
				fmt.Printf("[%s] Source not found but was in_progress — marking as migrated\n", dbName)
				stateMu.Lock()
				ds.Status = "migrated"
				ds.CompletedAt = time.Now()
				state.Databases[dbName] = ds
				err := migrate.SaveState(state, pebbleDataDir)
				stateMu.Unlock()
				return err
			}
			fmt.Printf("[%s] Warning: LevelDB not found, skipping\n", dbName)
			return nil
		}

		stateMu.Lock()
		ds.Status = "in_progress"
		state.Databases[dbName] = ds
		if err := migrate.SaveState(state, pebbleDataDir); err != nil {
			stateMu.Unlock()
			return fmt.Errorf("[%s] failed to save state: %w", dbName, err)
		}
		stateMu.Unlock()

		fmt.Printf("[%s] Starting migration...\n", dbName)
		keys, bytesMigrated, err := migrateSingleDB(ctx, dbName, dataDir, pebbleDataDir, opts)
		if err != nil {
			return fmt.Errorf("[%s] migration failed: %w", dbName, err)
		}

		stateMu.Lock()
		ds.Status = "migrated"
		ds.KeysMigrated = keys
		ds.BytesMigrated = bytesMigrated
		ds.CompletedAt = time.Now()
		state.Databases[dbName] = ds
		if err := migrate.SaveState(state, pebbleDataDir); err != nil {
			stateMu.Unlock()
			return fmt.Errorf("[%s] failed to save state: %w", dbName, err)
		}
		stateMu.Unlock()

		if !opts.skipVerify {
			fmt.Printf("[%s] Verifying...\n", dbName)
			if opts.verifyFull {
				if err := verifyDBFull(dbName, dataDir, pebbleDataDir, keys); err != nil {
					return fmt.Errorf("[%s] full verification failed: %w", dbName, err)
				}
			} else {
				if err := verifyDBSample(dbName, dataDir, pebbleDataDir, 1000, keys); err != nil {
					return fmt.Errorf("[%s] sample verification failed: %w", dbName, err)
				}
			}
			fmt.Printf("[%s] Verification passed\n", dbName)
		}

		if opts.noBackup {
			srcPath := filepath.Join(dataDir, dbName+".db")
			fmt.Printf("[%s] Removing source LevelDB: %s\n", dbName, srcPath)
			if err := os.RemoveAll(srcPath); err != nil {
				return fmt.Errorf("[%s] failed to remove source: %w", dbName, err)
			}
			stateMu.Lock()
			ds.Status = "source_deleted"
			state.Databases[dbName] = ds
			if err := migrate.SaveState(state, pebbleDataDir); err != nil {
				stateMu.Unlock()
				return fmt.Errorf("[%s] failed to save state: %w", dbName, err)
			}
			stateMu.Unlock()
		}

		fmt.Printf("[%s] Complete: %d keys, %s\n", dbName, keys, migrate.HumanBytes(bytesMigrated))
		return nil
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.parallel)
	for _, dbName := range databases {
		dbName := dbName
		g.Go(func() error {
			return migrateOne(gctx, dbName)
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	if opts.autoSwap {
		return performAutoSwap(opts.homeDir, dataDir, pebbleDataDir, opts.noBackup)
	}

	printNextSteps(dataDir, pebbleDataDir, opts.noBackup)
	return nil
}

// migrateSingleDB opens source/dest databases and copies data using CopyDB.
// It handles --no-backup incremental deletion which is specific to the offline tool.
func migrateSingleDB(ctx context.Context, dbName, sourceDir, destDir string, opts migrateOpts) (int64, int64, error) {
	startTime := time.Now()
	batchBytes := int64(opts.batchSizeMB) * 1024 * 1024
	syncBytes := int64(opts.syncInterval) * 1024 * 1024

	sourceDB, err := db.NewDB(dbName, db.GoLevelDBBackend, sourceDir)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open source LevelDB: %w", err)
	}
	defer sourceDB.Close()

	destDB, err := db.NewDB(dbName, db.PebbleDBBackend, destDir)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open destination PebbleDB: %w", err)
	}
	defer destDB.Close()

	// If no-backup is not needed, use the simple CopyDB path
	if !opts.noBackup {
		lastLogTime := time.Now()
		result, err := migrate.CopyDB(ctx, sourceDB, destDB, migrate.CopyDBOptions{
			BatchBytes:        batchBytes,
			SyncIntervalBytes: syncBytes,
			ProgressFn: func(keys, bytesTotal int64) {
				if time.Since(lastLogTime) >= 2*time.Minute {
					elapsed := time.Since(startTime)
					rate := float64(bytesTotal) / elapsed.Seconds()
					fmt.Printf("[%s] %d keys, %s migrated, %s/s, elapsed %s\n",
						dbName, keys, migrate.HumanBytes(bytesTotal), migrate.HumanBytes(int64(rate)),
						elapsed.Round(time.Second))
					lastLogTime = time.Now()
				}
			},
		})
		if err != nil {
			return 0, 0, err
		}
		elapsed := time.Since(startTime)
		fmt.Printf("[%s] Migration complete: %d keys, %s, elapsed %s\n",
			dbName, result.KeysCopied, migrate.HumanBytes(result.BytesCopied), elapsed.Round(time.Second))
		return result.KeysCopied, result.BytesCopied, nil
	}

	// --no-backup path: uses incremental source deletion
	return migrateSingleDBWithDelete(ctx, dbName, sourceDB, destDB, batchBytes, syncBytes, startTime)
}

// migrateSingleDBWithDelete handles the --no-backup mode where source keys are
// deleted incrementally after being copied to save disk space.
func migrateSingleDBWithDelete(ctx context.Context, dbName string, sourceDB, destDB db.DB, batchBytes, syncBytes int64, startTime time.Time) (int64, int64, error) {
	resumeKey, resumedKeys, err := migrate.FindResumePoint(destDB)
	if err != nil {
		return 0, 0, err
	}

	if resumeKey != nil {
		fmt.Printf("[%s] Resuming from key (already migrated: %d keys)\n", dbName, resumedKeys)
	}

	totalKeys := resumedKeys
	var totalBytes int64
	var bytesSinceSync int64
	var deleteKeys [][]byte
	var bytesSinceDelete int64
	lastLogTime := time.Now()

	srcIter, err := migrate.IteratorFrom(sourceDB, resumeKey)
	if err != nil {
		return 0, 0, err
	}

	batch := destDB.NewBatch()
	var batchKeyCount int

	flushBatch := func(doSync bool) error {
		if batchKeyCount == 0 {
			return nil
		}
		var newBatch db.Batch
		newBatch, err = migrate.FlushBatch(batch, destDB, doSync)
		if err != nil {
			return err
		}
		batch = newBatch
		batchKeyCount = 0
		return nil
	}

	for ; srcIter.Valid(); srcIter.Next() {
		key := srcIter.Key()
		value := srcIter.Value()
		kvSize := int64(len(key) + len(value))

		if err := batch.Set(key, value); err != nil {
			srcIter.Close()
			batch.Close()
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
			if err := flushBatch(needSync); err != nil {
				srcIter.Close()
				return 0, 0, err
			}
			if needSync {
				bytesSinceSync = 0
			}

			select {
			case <-ctx.Done():
				srcIter.Close()
				return 0, 0, fmt.Errorf("cancelled: %w", ctx.Err())
			default:
			}
		}

		if time.Since(lastLogTime) >= 2*time.Minute {
			elapsed := time.Since(startTime)
			rate := float64(totalBytes) / elapsed.Seconds()
			fmt.Printf("[%s] %d keys, %s migrated, %s/s, elapsed %s\n",
				dbName, totalKeys, migrate.HumanBytes(totalBytes), migrate.HumanBytes(int64(rate)),
				elapsed.Round(time.Second))
			lastLogTime = time.Now()
		}

		if bytesSinceDelete >= deleteChunkBytes {
			if err := flushBatch(true); err != nil {
				srcIter.Close()
				return 0, 0, err
			}
			bytesSinceSync = 0

			lastKey := make([]byte, len(key))
			copy(lastKey, key)
			srcIter.Close()

			if err := migrate.DeleteSourceKeys(sourceDB, deleteKeys); err != nil {
				return 0, 0, fmt.Errorf("failed to delete source keys: %w", err)
			}
			deleteKeys = deleteKeys[:0]
			bytesSinceDelete = 0

			srcIter, err = migrate.IteratorFrom(sourceDB, lastKey)
			if err != nil {
				return 0, 0, err
			}
		}
	}

	if err := srcIter.Error(); err != nil {
		srcIter.Close()
		batch.Close()
		return 0, 0, fmt.Errorf("iterator error: %w", err)
	}
	srcIter.Close()

	if err := flushBatch(true); err != nil {
		return 0, 0, err
	}

	if len(deleteKeys) > 0 {
		if err := migrate.DeleteSourceKeys(sourceDB, deleteKeys); err != nil {
			return 0, 0, fmt.Errorf("failed to delete remaining source keys: %w", err)
		}
	}

	elapsed := time.Since(startTime)
	newKeys := totalKeys - resumedKeys
	fmt.Printf("[%s] Migration complete: %d keys total (%d new), %s, elapsed %s\n",
		dbName, totalKeys, newKeys, migrate.HumanBytes(totalBytes), elapsed.Round(time.Second))

	return totalKeys, totalBytes, nil
}

func verifyDBSample(dbName, sourceDir, destDir string, sampleSize int, knownKeyCount int64) error {
	sourceDB, err := db.NewDB(dbName, db.GoLevelDBBackend, sourceDir)
	if err != nil {
		return fmt.Errorf("failed to open source for verification: %w", err)
	}
	defer sourceDB.Close()

	destDB, err := db.NewDB(dbName, db.PebbleDBBackend, destDir)
	if err != nil {
		return fmt.Errorf("failed to open dest for verification: %w", err)
	}
	defer destDB.Close()

	if knownKeyCount == 0 {
		fmt.Printf("[%s] Source is empty, nothing to verify\n", dbName)
		return nil
	}

	stride := knownKeyCount / int64(sampleSize)
	if stride < 1 {
		stride = 1
	}

	// Merge-walk: iterate both source and dest in lockstep.
	srcIter, err := sourceDB.Iterator(nil, nil)
	if err != nil {
		return err
	}
	defer srcIter.Close()

	destIter, err := destDB.Iterator(nil, nil)
	if err != nil {
		return err
	}
	defer destIter.Close()

	var checked, mismatches, idx int64
	for ; srcIter.Valid() && destIter.Valid(); srcIter.Next() {
		srcKey := srcIter.Key()
		destKey := destIter.Key()

		cmp := bytes.Compare(srcKey, destKey)
		if cmp != 0 {
			mismatches++
			if mismatches <= 3 {
				fmt.Printf("[%s] Key mismatch at index %d: src=%x dest=%x\n", dbName, idx, srcKey, destKey)
			}
			// Advance the lagging iterator to try to resync.
			if cmp > 0 {
				destIter.Next()
			}
			idx++
			continue
		}

		// Keys match. Compare values on sampled keys.
		if idx%stride == 0 {
			if !bytes.Equal(srcIter.Value(), destIter.Value()) {
				mismatches++
				if mismatches <= 3 {
					fmt.Printf("[%s] Value mismatch at key %x\n", dbName, srcKey)
				}
			}
			checked++
		}

		destIter.Next()
		idx++
	}

	// Check for extra keys on either side.
	if srcIter.Valid() && !destIter.Valid() {
		mismatches++
		fmt.Printf("[%s] Source has more keys than dest (dest exhausted at index %d)\n", dbName, idx)
	} else if !srcIter.Valid() && destIter.Valid() {
		mismatches++
		fmt.Printf("[%s] Dest has more keys than source (source exhausted at index %d)\n", dbName, idx)
	}

	if mismatches > 0 {
		return fmt.Errorf("sample verification found %d mismatches out of %d value-checked (%d keys walked)", mismatches, checked, idx)
	}
	fmt.Printf("[%s] Sample verification passed: %d/%d values checked, %d keys walked\n", dbName, checked, knownKeyCount, idx)
	return nil
}

func verifyDBFull(dbName, sourceDir, destDir string, expectedCount int64) error {
	sourceDB, err := db.NewDB(dbName, db.GoLevelDBBackend, sourceDir)
	if err != nil {
		return fmt.Errorf("failed to open source for full verification: %w", err)
	}
	defer sourceDB.Close()

	destDB, err := db.NewDB(dbName, db.PebbleDBBackend, destDir)
	if err != nil {
		return fmt.Errorf("failed to open PebbleDB for verification: %w", err)
	}
	defer destDB.Close()

	srcIter, err := sourceDB.Iterator(nil, nil)
	if err != nil {
		return err
	}
	defer srcIter.Close()

	destIter, err := destDB.Iterator(nil, nil)
	if err != nil {
		return err
	}
	defer destIter.Close()

	var verified int64
	var mismatches int64
	for ; srcIter.Valid() && destIter.Valid(); srcIter.Next() {
		srcKey := srcIter.Key()
		destKey := destIter.Key()

		if !bytes.Equal(srcKey, destKey) {
			mismatches++
			if mismatches <= 3 {
				fmt.Printf("[%s] Key mismatch at index %d: src=%x dest=%x\n", dbName, verified, srcKey, destKey)
			}
			// Try to resync by advancing the lagging iterator.
			if bytes.Compare(srcKey, destKey) > 0 {
				destIter.Next()
			}
			verified++
			continue
		}

		if !bytes.Equal(srcIter.Value(), destIter.Value()) {
			mismatches++
			if mismatches <= 3 {
				fmt.Printf("[%s] Value mismatch at key %x\n", dbName, srcKey)
			}
		}

		destIter.Next()
		verified++

		if verified%1000000 == 0 {
			fmt.Printf("[%s] Verified %d keys...\n", dbName, verified)
		}
	}

	// Check for leftover keys.
	if srcIter.Valid() {
		mismatches++
		fmt.Printf("[%s] Source has keys remaining after dest exhausted at %d\n", dbName, verified)
	}
	if destIter.Valid() {
		mismatches++
		fmt.Printf("[%s] Dest has keys remaining after source exhausted at %d\n", dbName, verified)
	}

	if mismatches > 0 {
		return fmt.Errorf("full verification failed: %d mismatches found across %d keys", mismatches, verified)
	}

	if verified != expectedCount {
		return fmt.Errorf("verification failed: expected %d keys, walked %d keys", expectedCount, verified)
	}

	fmt.Printf("[%s] Full verification passed: %d keys, all values match\n", dbName, verified)
	return nil
}

func performAutoSwap(homeDir, dataDir, pebbleDataDir string, noBackup bool) error {
	fmt.Println("\nPerforming auto-swap...")

	for _, dbName := range migrate.AllDatabases {
		srcPath := filepath.Join(pebbleDataDir, dbName+".db")
		dstPath := filepath.Join(dataDir, dbName+".db")

		if !noBackup {
			if _, err := os.Stat(dstPath); err == nil {
				fmt.Printf("  Removing old %s\n", dstPath)
				if err := os.RemoveAll(dstPath); err != nil {
					return fmt.Errorf("failed to remove old %s: %w", dstPath, err)
				}
			}
		}

		if _, err := os.Stat(srcPath); err == nil {
			fmt.Printf("  Moving %s -> %s\n", srcPath, dstPath)
			if err := os.Rename(srcPath, dstPath); err != nil {
				return fmt.Errorf("failed to move %s: %w", dbName, err)
			}
		}
	}

	configPath := filepath.Join(homeDir, "config", "config.toml")
	if err := updateConfigBackend(configPath, "pebbledb"); err != nil {
		fmt.Printf("  Warning: could not update config.toml: %v\n", err)
		fmt.Printf("  Please manually set db_backend = \"pebbledb\" in %s\n", configPath)
	} else {
		fmt.Printf("  Updated %s: db_backend = \"pebbledb\"\n", configPath)
	}

	os.Remove(filepath.Join(pebbleDataDir, ".migration_state.json"))
	os.Remove(filepath.Join(pebbleDataDir, ".migration.lock"))
	os.Remove(pebbleDataDir)

	fmt.Println("\nAuto-swap complete. Start your node to verify.")
	return nil
}

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

func printNextSteps(dataDir, pebbleDataDir string, noBackup bool) {
	var rmCommands, mvCommands strings.Builder
	for _, dbName := range migrate.AllDatabases {
		if !noBackup {
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
	if !noBackup {
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
