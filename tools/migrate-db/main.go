package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"hash"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	cmtcfg "github.com/cometbft/cometbft/config"
	db "github.com/cosmos/cosmos-db"
	"github.com/gofrs/flock"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"
)

const (
	// defaultDeleteChunkMB is the default amount of data (in MB) migrated before
	// deleting source keys when backup is disabled.
	defaultDeleteChunkMB = 1024 // 1 GB

	// maxDeleteBatch is the maximum size of a single delete batch written to the source DB.
	maxDeleteBatch = 64 * 1024 * 1024 // 64 MB

	// maxDeleteKeys caps how many keys are buffered in memory before a delete
	// chunk is forced, regardless of byte size. Prevents unbounded memory growth
	// when keys/values are tiny (millions of [][]byte copies otherwise).
	maxDeleteKeys = 2_000_000

	// progressInterval is how often progress is logged during migration.
	progressInterval = 2 * time.Minute

	// stagingDirName is the per-source-directory subdirectory that holds
	// in-progress PebbleDB copies. Keeping staging in the same directory as the
	// source guarantees the final swap is an atomic same-filesystem rename even
	// when the consensus DBs live on a custom db_dir mount.
	stagingDirName = ".pebble-migrate"

	// backupSuffix is appended to a source LevelDB directory that is preserved
	// (--backup) or temporarily moved aside during the swap for rollback.
	backupSuffix = ".leveldb-bak"

	pebbleBackendName  = "pebbledb"
	levelDBBackendName = "goleveldb"
	appDBBackendKey    = "app-db-backend" // app.toml; takes precedence (SDK GetAppDBBackend)
	configDBBackendKey = "db_backend"     // config.toml
	migrationStateFile = ".migration_state.json"
	migrationLockFile  = ".migration.lock"
)

// dbTarget describes a single database to migrate, including where its files
// live on disk. Different databases honor different configuration:
//   - application + snapshots/metadata are opened by the SDK under <home>/data
//     (and <home>/data/snapshots) using the app DB backend.
//   - blockstore/state/evidence/tx_index are opened by CometBFT under cfg.DBDir()
//     (config.toml db_dir, default "data") using db_backend.
type dbTarget struct {
	// name is the unique logical identifier used in state and logs.
	name string
	// fileName is the on-disk base name; the directory is named fileName+".db".
	fileName string
	// dir is the directory that contains (or will contain) fileName+".db".
	dir string
	// optional indicates the database may legitimately be absent.
	optional bool
}

func (t dbTarget) srcPath() string    { return filepath.Join(t.dir, t.fileName+".db") }
func (t dbTarget) stagingDir() string { return filepath.Join(t.dir, stagingDirName) }
func (t dbTarget) stagedPath() string { return filepath.Join(t.stagingDir(), t.fileName+".db") }
func (t dbTarget) backupPath() string { return filepath.Join(t.dir, t.fileName+".db"+backupSuffix) }

// MigrationState tracks overall migration progress across restarts.
type MigrationState struct {
	StartedAt        time.Time          `json:"started_at"`
	Backup           bool               `json:"backup"`
	DeleteChunkBytes int64              `json:"delete_chunk_bytes"`
	Databases        map[string]DBState `json:"databases"`
}

// stateTracker bundles MigrationState with a mutex for concurrent access and the state dir for persistence.
type stateTracker struct {
	mu       sync.Mutex
	state    *MigrationState
	stateDir string
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
	return saveState(st.state, st.stateDir)
}

type dbStatus string

const (
	statusPending    dbStatus = "pending"
	statusInProgress dbStatus = "in_progress"
	statusMigrated   dbStatus = "migrated"
	statusVerified   dbStatus = "verified"
	statusSwapped    dbStatus = "swapped"
	statusNotFound   dbStatus = "not_found"
)

// DBState tracks the migration status of a single database.
type DBState struct {
	Status            dbStatus  `json:"status"`
	KeysMigrated      int64     `json:"keys_migrated"`
	BytesMigrated     int64     `json:"bytes_migrated"`
	SourceKeysDeleted int64     `json:"source_keys_deleted"`
	CompletedAt       time.Time `json:"completed_at"`
}

type migrateOpts struct {
	homeDir       string
	dryRun        bool
	backup        bool
	batchSizeMB   int
	deleteChunkMB int
	parallel      int
	dbFilter      string
	manualSwap    bool
	skipCompact   bool
	check         bool
}

func main() {
	opts := migrateOpts{}
	flag.StringVar(&opts.homeDir, "home", os.ExpandEnv("$HOME/.celestia-app"), "Node home directory")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "Run migration in dry-run mode without making changes")
	flag.BoolVar(&opts.backup, "backup", false, "Keep source LevelDB data after migration (default: delete incrementally)")
	flag.IntVar(&opts.batchSizeMB, "batch-size", 64, "Batch size in MB")
	flag.IntVar(&opts.deleteChunkMB, "delete-chunk", defaultDeleteChunkMB, "Delete source keys every N MB migrated (no-backup mode)")
	flag.IntVar(&opts.parallel, "parallel", 3, "Migrate N databases concurrently")
	flag.StringVar(&opts.dbFilter, "db", "", "Migrate only a specific database (e.g. --db blockstore)")
	flag.BoolVar(&opts.manualSwap, "manual-swap", false, "Skip auto-swap; print manual instructions instead")
	flag.BoolVar(&opts.skipCompact, "skip-compact", false, "Skip post-migration compaction (not recommended)")
	flag.BoolVar(&opts.check, "check", false, "Do not migrate; open the existing (PebbleDB) databases and verify they are consistent")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: migrate-db [options]

Migrate celestia-app databases from LevelDB to PebbleDB.

This tool is resumable and idempotent. If interrupted, simply re-run
to continue from where it left off. On resume, the last written key
is verified against the source before continuing.

By default, source data is deleted incrementally as it is migrated
and databases are auto-swapped into place after migration. Every
destination database is reopened and its consistency verified before
the source is destroyed or swapped.

Databases migrated (locations resolved from config):
- application.db          (<home>/data, app-db-backend)
- snapshots/metadata.db   (<home>/data/snapshots, app-db-backend)
- blockstore/state/evidence/tx_index.db (cfg.DBDir(), db_backend)

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

// resolveTargets builds the list of databases to migrate, resolving each one's
// on-disk directory from the node configuration.
func resolveTargets(homeDir string) ([]dbTarget, error) {
	cfg, err := loadCometConfig(homeDir)
	if err != nil {
		return nil, err
	}
	dataDir := filepath.Join(homeDir, "data")
	cometDir := cfg.DBDir() // honors config.toml db_dir; defaults to <home>/data
	snapshotsDir := filepath.Join(dataDir, "snapshots")

	return []dbTarget{
		{name: "application", fileName: "application", dir: dataDir},
		{name: "blockstore", fileName: "blockstore", dir: cometDir},
		{name: "state", fileName: "state", dir: cometDir},
		{name: "tx_index", fileName: "tx_index", dir: cometDir, optional: true},
		{name: "evidence", fileName: "evidence", dir: cometDir},
		{name: "snapshots/metadata", fileName: "metadata", dir: snapshotsDir, optional: true},
	}, nil
}

func targetNames(targets []dbTarget) []string {
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = t.name
	}
	return names
}

// runMigration orchestrates the full LevelDB-to-PebbleDB migration.
func runMigration(ctx context.Context, opts migrateOpts) error {
	dataDir := filepath.Join(opts.homeDir, "data")
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		return fmt.Errorf("data directory does not exist: %s", dataDir)
	}

	targets, err := resolveTargets(opts.homeDir)
	if err != nil {
		return fmt.Errorf("failed to resolve database locations: %w", err)
	}

	backend, err := effectiveBackend(opts.homeDir)
	if err != nil {
		return fmt.Errorf("failed to read node configuration: %w", err)
	}

	// --check: open the (expected PebbleDB) databases and verify consistency.
	if opts.check {
		return runCheck(targets, backend)
	}

	if opts.dbFilter != "" {
		if !slices.Contains(targetNames(targets), opts.dbFilter) {
			return fmt.Errorf("unknown database %q, valid options: %s", opts.dbFilter, strings.Join(targetNames(targets), ", "))
		}
		targets = filterTargets(targets, opts.dbFilter)
	}

	// State directory holds the lock + resumable migration state (not the DBs,
	// which are staged in-place next to each source).
	stateDir := filepath.Join(opts.homeDir, "data_pebble")

	if backend == pebbleBackendName {
		// Already on PebbleDB. Do not silently exit — a partial migration may
		// have flipped the config. Resume the swap/verify if state exists.
		if st, _ := loadState(stateDir); st != nil {
			fmt.Printf("Config already reports %q, but a migration state file exists — resuming.\n", pebbleBackendName)
		} else {
			fmt.Printf("Config already reports db_backend/app-db-backend = %q. Nothing to migrate.\n", pebbleBackendName)
			fmt.Printf("Run with --check to verify the existing databases open consistently.\n")
			return nil
		}
	}

	printBanner(opts, targets)

	if opts.dryRun {
		return dryRun(targets, backend)
	}

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Acquire kernel-level file lock (automatically released on crash).
	lockPath := filepath.Join(stateDir, migrationLockFile)
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

	tracker, err := loadOrInitState(stateDir, targets, opts)
	if err != nil {
		return err
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.parallel)
	for _, t := range targets {
		g.Go(func() error {
			return migrateOneDB(gctx, t, tracker, opts)
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	if opts.manualSwap {
		printNextSteps(targets, opts.backup, tracker)
		return nil
	}

	// Auto-swap requires the full set of databases; a --db filter only ever
	// migrates a subset, so it can't flip the node's config safely.
	if opts.dbFilter != "" {
		fmt.Println("\nSkipping auto-swap: a --db filter is active.")
		fmt.Println("Re-run without --db to migrate all databases, or use --manual-swap.")
		return nil
	}

	return performAutoSwap(opts.homeDir, targets, tracker, opts.backup)
}

func filterTargets(targets []dbTarget, name string) []dbTarget {
	for _, t := range targets {
		if t.name == name {
			return []dbTarget{t}
		}
	}
	return nil
}

// printBanner prints the migration configuration summary.
func printBanner(opts migrateOpts, targets []dbTarget) {
	fmt.Printf("Starting database migration from LevelDB to PebbleDB\n")
	fmt.Printf("Home directory:    %s\n", opts.homeDir)
	fmt.Printf("Backup:            %v\n", opts.backup)
	fmt.Printf("Batch size:        %d MB\n", opts.batchSizeMB)
	fmt.Printf("Delete chunk:      %d MB\n", opts.deleteChunkMB)
	fmt.Printf("Parallel:          %d\n", opts.parallel)
	fmt.Printf("Databases:\n")
	for _, t := range targets {
		fmt.Printf("  - %-20s %s\n", t.name, t.srcPath())
	}
	fmt.Println()
}

func dryRun(targets []dbTarget, backend string) error {
	for _, t := range targets {
		if _, err := os.Stat(t.srcPath()); os.IsNotExist(err) {
			fmt.Printf("[%s] LevelDB not found at %s, would skip\n", t.name, t.srcPath())
			continue
		}
		if isPebbleDB(t.srcPath()) {
			return fmt.Errorf("[%s] source database is already PebbleDB but config reports %q — resolve this inconsistency before migrating", t.name, backend)
		}
		fmt.Printf("[%s] Would migrate %s -> (staged) %s -> swap into place\n", t.name, t.srcPath(), t.stagedPath())
	}
	fmt.Println("\nDry-run complete. No changes were made.")
	return nil
}

// loadOrInitState loads existing migration state or creates a fresh one.
func loadOrInitState(stateDir string, targets []dbTarget, opts migrateOpts) (*stateTracker, error) {
	state, err := loadState(stateDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load migration state: %w", err)
	}

	deleteChunkBytes := int64(opts.deleteChunkMB) * 1024 * 1024
	if state == nil {
		// Starting fresh. Refuse to run if staged PebbleDB data already exists:
		// the state file (in data_pebble) and the staging dirs (next to each
		// source) are separate, so removing data_pebble alone orphans staging.
		// Reusing it would resume from keys that may not match this source — and
		// in no-backup mode the staged data could be the only copy of source keys
		// a previous run already deleted. Require an explicit cleanup instead.
		for _, t := range targets {
			if isPebbleDB(t.stagedPath()) {
				return nil, fmt.Errorf("found stale staging data at %s but no migration state in %s: "+
					"if a previous migration was interrupted, restore its %s to resume safely; "+
					"otherwise remove the stale staging directories (rm -rf the %q dirs) and re-run",
					t.stagedPath(), stateDir, migrationStateFile, stagingDirName)
			}
		}
		state = &MigrationState{
			StartedAt:        time.Now(),
			Backup:           opts.backup,
			DeleteChunkBytes: deleteChunkBytes,
			Databases:        make(map[string]DBState),
		}
		for _, t := range targets {
			state.Databases[t.name] = DBState{Status: statusPending}
		}
		if err := saveState(state, stateDir); err != nil {
			return nil, err
		}
		fmt.Println("Initialized new migration state.")
	} else {
		// Validate backup mode consistency on resume to prevent accidental data deletion.
		if state.Backup && !opts.backup {
			return nil, fmt.Errorf("migration was started with --backup but resumed without it; pass --backup to continue (or delete %s to start fresh)", filepath.Join(stateDir, migrationStateFile))
		}
		if !state.Backup && opts.backup {
			return nil, fmt.Errorf("migration was started without --backup (source already being deleted) but resumed with --backup; re-run without --backup to continue")
		}
		if state.DeleteChunkBytes > 0 && state.DeleteChunkBytes != deleteChunkBytes {
			fmt.Printf("Note: using persisted --delete-chunk=%d MB (ignoring current flag value %d MB)\n",
				state.DeleteChunkBytes/(1024*1024), opts.deleteChunkMB)
		}
		// Make sure any newly-resolved targets are present in the state map.
		for _, t := range targets {
			if _, ok := state.Databases[t.name]; !ok {
				state.Databases[t.name] = DBState{Status: statusPending}
			}
		}
		fmt.Printf("Resuming migration started at %s\n", state.StartedAt.Format(time.RFC3339))
		for name, ds := range state.Databases {
			if ds.Status != statusPending {
				fmt.Printf("  [%s] status=%s keys=%d bytes=%s\n", name, ds.Status, ds.KeysMigrated, humanBytes(ds.BytesMigrated))
			}
		}
		fmt.Println()
	}

	return &stateTracker{state: state, stateDir: stateDir}, nil
}

// migrateOneDB handles a single database end-to-end: state transitions, copy, verify, and source cleanup.
func migrateOneDB(ctx context.Context, t dbTarget, tracker *stateTracker, opts migrateOpts) error {
	ds := tracker.getDBState(t.name)

	// Already done (verified/swapped/source-deleted): nothing to do.
	if ds.Status == statusVerified || ds.Status == statusSwapped || ds.Status == statusNotFound {
		fmt.Printf("[%s] Already complete (status=%s), skipping\n", t.name, ds.Status)
		return nil
	}

	srcExists := true
	if _, err := os.Stat(t.srcPath()); os.IsNotExist(err) {
		srcExists = false
	}

	// Source missing.
	if !srcExists {
		switch {
		case ds.Status == statusInProgress || ds.Status == statusMigrated:
			// No-backup crash recovery: source was deleted, dest holds the data.
			fmt.Printf("[%s] Source not found but was %s — proceeding to verification\n", t.name, ds.Status)
		case t.optional:
			fmt.Printf("[%s] LevelDB not found, skipping (optional)\n", t.name)
			ds.Status = statusNotFound
			return tracker.updateDBState(t.name, ds)
		default:
			// A required database is missing and was never migrated. Fail loudly
			// so auto-swap cannot flip the config with a database absent.
			return fmt.Errorf("[%s] required database not found at %s — refusing to migrate (resolve before continuing)", t.name, t.srcPath())
		}
	} else if isPebbleDB(t.srcPath()) {
		return fmt.Errorf("[%s] source database at %s is already PebbleDB — resolve this inconsistency before migrating", t.name, t.srcPath())
	}

	// Run the copy unless already migrated (resume after a crash post-copy).
	if ds.Status != statusMigrated {
		ds.Status = statusInProgress
		if err := tracker.updateDBState(t.name, ds); err != nil {
			return fmt.Errorf("[%s] failed to save state: %w", t.name, err)
		}

		if err := os.MkdirAll(t.stagingDir(), 0o755); err != nil {
			return fmt.Errorf("[%s] failed to create staging dir: %w", t.name, err)
		}

		// recordDeleted persists the running source-deletion count after every
		// chunk so a crash mid-copy leaves an accurate lower bound for the
		// no-backup verification (rather than only saving it at DB end).
		recordDeleted := func(n int64) error {
			ds.SourceKeysDeleted += n
			return tracker.updateDBState(t.name, ds)
		}

		fmt.Printf("[%s] Starting migration...\n", t.name)
		keys, bytesMigrated, err := migrateSingleDB(ctx, t, tracker.state.DeleteChunkBytes, opts, recordDeleted)
		if err != nil {
			return fmt.Errorf("[%s] migration failed: %w", t.name, err)
		}

		// SourceKeysDeleted was already accumulated via recordDeleted. Keys/bytes
		// accumulate across resumed runs (a crash mid-copy may split a DB's
		// migration over several invocations).
		ds.Status = statusMigrated
		ds.KeysMigrated += keys
		ds.BytesMigrated += bytesMigrated
		ds.CompletedAt = time.Now()
		if err := tracker.updateDBState(t.name, ds); err != nil {
			return fmt.Errorf("[%s] failed to save state: %w", t.name, err)
		}
	}

	// Mandatory verification before any irreversible step.
	fmt.Printf("[%s] Verifying destination...\n", t.name)
	if err := verifyDestination(t, ds, opts); err != nil {
		return fmt.Errorf("[%s] verification failed: %w", t.name, err)
	}
	ds.Status = statusVerified
	if err := tracker.updateDBState(t.name, ds); err != nil {
		return fmt.Errorf("[%s] failed to save state: %w", t.name, err)
	}
	fmt.Printf("[%s] Verification passed\n", t.name)

	// In no-backup mode the source was deleted incrementally during the copy.
	// Remove any residual source directory now that the dest is verified.
	if !opts.backup && srcExists {
		fmt.Printf("[%s] Removing residual source LevelDB: %s\n", t.name, t.srcPath())
		if err := os.RemoveAll(t.srcPath()); err != nil {
			return fmt.Errorf("[%s] failed to remove source: %w", t.name, err)
		}
	}

	fmt.Printf("[%s] Complete: %d keys, %s\n", t.name, ds.KeysMigrated, humanBytes(ds.BytesMigrated))
	return nil
}

// migrateSingleDB opens source and dest DBs, finds the resume point, and copies
// keys. It returns the keys and bytes copied in this run. In no-backup mode it
// reports each deleted chunk through recordDeleted as it goes.
func migrateSingleDB(ctx context.Context, t dbTarget, deleteChunkBytes int64, opts migrateOpts, recordDeleted func(int64) error) (int64, int64, error) {
	startTime := time.Now()
	batchBytes := int64(opts.batchSizeMB) * 1024 * 1024

	sourceDB, err := db.NewDB(t.fileName, db.GoLevelDBBackend, t.dir)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open source LevelDB: %w", err)
	}
	defer func() { _ = sourceDB.Close() }()

	destDB, err := db.NewDB(t.fileName, db.PebbleDBBackend, t.stagingDir())
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open destination PebbleDB: %w", err)
	}
	pdb, ok := destDB.(*db.PebbleDB)
	if !ok {
		_ = destDB.Close()
		return 0, 0, fmt.Errorf("destination is not a PebbleDB")
	}
	destClosed := false
	closeDest := func() error {
		if destClosed {
			return nil
		}
		destClosed = true
		// Close the underlying pebble handle directly so the close error (which
		// the cosmos-db wrapper swallows) is surfaced.
		return pdb.DB().Close()
	}
	defer func() { _ = closeDest() }()

	resumeKey, err := findResumePoint(destDB, t.name)
	if err != nil {
		return 0, 0, err
	}
	if err := verifyResumePoint(sourceDB, destDB, resumeKey, t.name); err != nil {
		return 0, 0, err
	}

	srcIter, err := iteratorFrom(sourceDB, resumeKey)
	if err != nil {
		return 0, 0, err
	}

	var totalKeys, totalBytes int64
	if !opts.backup {
		// recordDeleted persists the deletion count as it goes; the returned total is unused here.
		totalKeys, totalBytes, _, err = copyAndDeleteKeys(ctx, t, sourceDB, destDB, srcIter, batchBytes, deleteChunkBytes, recordDeleted, startTime)
	} else {
		totalKeys, totalBytes, err = copyAllKeys(ctx, t.name, destDB, srcIter, batchBytes, startTime)
	}
	if err != nil {
		return 0, 0, err
	}

	// Force all memtables to durable SSTs so the on-disk LSM is complete and
	// self-consistent before we compact/close. Without this the data only lives
	// in the (NoSync) WAL and a non-graceful stop can leave the MANIFEST
	// referencing files that were never written.
	if err := pdb.DB().Flush(); err != nil {
		return 0, 0, fmt.Errorf("[%s] flush failed: %w", t.name, err)
	}

	if !opts.skipCompact {
		if err := compactPebbleDB(t.name, destDB); err != nil {
			return 0, 0, err
		}
	}

	// Checked close — propagate any close error instead of swallowing it.
	if err := closeDest(); err != nil {
		return 0, 0, fmt.Errorf("[%s] failed to close destination cleanly: %w", t.name, err)
	}

	elapsed := time.Since(startTime)
	fmt.Printf("[%s] Migration complete: %d keys copied, %s, elapsed %s\n",
		t.name, totalKeys, humanBytes(totalBytes), elapsed.Round(time.Second))

	return totalKeys, totalBytes, nil
}

// compactPebbleDB runs a full compaction on the destination PebbleDB to reduce disk bloat
// from bulk batch writes that create many overlapping SST files.
func compactPebbleDB(dbName string, destDB db.DB) error {
	pdb, ok := destDB.(*db.PebbleDB)
	if !ok {
		return fmt.Errorf("[%s] destination is not PebbleDB, cannot compact", dbName)
	}
	fmt.Printf("[%s] Compacting PebbleDB (this may take a while)...\n", dbName)
	start := time.Now()
	// Determine the actual maximum key so the whole keyspace is compacted; a
	// fixed small sentinel (e.g. 0xff*4) would exclude any keys sorting after it.
	end, hasKeys, err := maxKey(destDB)
	if err != nil {
		return fmt.Errorf("[%s] failed to determine key range for compaction: %w", dbName, err)
	}
	if !hasKeys {
		fmt.Printf("[%s] Compaction skipped (empty)\n", dbName)
		return nil
	}
	// Compact [nil, end] inclusive: append a 0x00 so the upper bound is strictly
	// greater than the largest key (pebble's Compact end is exclusive-ish).
	upper := make([]byte, 0, len(end)+1)
	upper = append(upper, end...)
	upper = append(upper, 0x00)
	if err := pdb.DB().Compact(nil, upper, true); err != nil {
		return fmt.Errorf("[%s] compaction failed: %w", dbName, err)
	}
	fmt.Printf("[%s] Compaction complete, elapsed %s\n", dbName, time.Since(start).Round(time.Second))
	return nil
}

// maxKey returns the largest key in the database, or hasKeys=false if empty.
func maxKey(d db.DB) ([]byte, bool, error) {
	it, err := d.ReverseIterator(nil, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = it.Close() }()
	if !it.Valid() {
		return nil, false, it.Error()
	}
	k := make([]byte, len(it.Key()))
	copy(k, it.Key())
	return k, true, nil
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
	if err := revIter.Close(); err != nil {
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

// flushBatch commits the batch asynchronously (NoSync), closes it, and returns a
// fresh batch. Durability is provided separately by Flush() at chunk boundaries
// (no-backup mode) and at the end of each database, so per-batch fsync is
// unnecessary and would only slow the copy down.
func flushBatch(batch db.Batch, destDB db.DB) (db.Batch, error) {
	if err := batch.Write(); err != nil {
		_ = batch.Close()
		return nil, fmt.Errorf("failed to write batch: %w", err)
	}
	if err := batch.Close(); err != nil {
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

// copyAllKeys copies all keys from srcIter into destDB in batches. Returns total
// keys and bytes copied. Batches are committed async; the caller's Flush() makes
// the result durable.
func copyAllKeys(ctx context.Context, dbName string, destDB db.DB, srcIter db.Iterator, batchBytes int64, startTime time.Time) (int64, int64, error) {
	var totalKeys, totalBytes int64
	lastLogTime := time.Now()

	batch := destDB.NewBatch()

	for ; srcIter.Valid(); srcIter.Next() {
		key := srcIter.Key()
		value := srcIter.Value()

		if err := batch.Set(key, value); err != nil {
			_ = srcIter.Close()
			_ = batch.Close()
			return 0, 0, fmt.Errorf("failed to set key in batch: %w", err)
		}

		totalKeys++
		totalBytes += int64(len(key) + len(value))

		if size, _ := batch.GetByteSize(); int64(size) >= batchBytes {
			var err error
			if batch, err = flushBatch(batch, destDB); err != nil {
				_ = srcIter.Close()
				return 0, 0, err
			}
		}

		if totalKeys%10000 == 0 {
			if err := ctx.Err(); err != nil {
				_ = srcIter.Close()
				_ = batch.Close()
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
	if err := srcIter.Close(); err != nil {
		_ = batch.Close()
		return 0, 0, fmt.Errorf("failed to close source iterator: %w", err)
	}

	if err := batch.Write(); err != nil {
		_ = batch.Close()
		return 0, 0, fmt.Errorf("failed to write final batch: %w", err)
	}
	_ = batch.Close()

	return totalKeys, totalBytes, nil
}

// copyAndDeleteKeys copies keys into destDB and incrementally deletes them from
// sourceDB in chunks to keep disk usage low. Each chunk is only deleted after the
// destination is durably flushed and the chunk's values are validated against the
// source (see deleteChunk), so a crash can never leave a key absent from both.
// Returns total keys copied, bytes copied, and source keys deleted (this run).
func copyAndDeleteKeys(ctx context.Context, t dbTarget, sourceDB, destDB db.DB, srcIter db.Iterator, batchBytes, deleteChunkBytes int64, recordDeleted func(int64) error, startTime time.Time) (int64, int64, int64, error) {
	dbName := t.name
	pdb, _ := destDB.(*db.PebbleDB)

	var totalKeys, totalBytes, bytesSinceDelete, totalDeleted int64
	var deleteKeys [][]byte
	var pending int // keys written to the current batch but not yet committed
	lastLogTime := time.Now()
	batch := destDB.NewBatch()

	fail := func(err error) (int64, int64, int64, error) {
		_ = srcIter.Close()
		_ = batch.Close()
		return 0, 0, 0, err
	}

	for srcIter.Valid() {
		key := srcIter.Key()
		value := srcIter.Value()

		if err := batch.Set(key, value); err != nil {
			return fail(fmt.Errorf("failed to set key in batch: %w", err))
		}
		totalKeys++
		pending++
		totalBytes += int64(len(key) + len(value))
		bytesSinceDelete += int64(len(key) + len(value))
		deleteKeys = append(deleteKeys, append([]byte(nil), key...))

		if size, _ := batch.GetByteSize(); int64(size) >= batchBytes {
			var err error
			if batch, err = flushBatch(batch, destDB); err != nil {
				return fail(err)
			}
			pending = 0
		}

		if totalKeys%10000 == 0 {
			if err := ctx.Err(); err != nil {
				return fail(fmt.Errorf("cancelled: %w", err))
			}
		}
		logProgress(dbName, totalKeys, totalBytes, startTime, &lastLogTime)

		// Delete a chunk on either byte size or key count (the latter bounds memory).
		if bytesSinceDelete >= deleteChunkBytes || len(deleteKeys) >= maxDeleteKeys {
			lastKey := append([]byte(nil), key...)
			// Commit pending writes and release the source iterator (LevelDB
			// iterators pin a snapshot) before deleting.
			if pending > 0 {
				var err error
				if batch, err = flushBatch(batch, destDB); err != nil {
					return fail(err)
				}
				pending = 0
			}
			if err := srcIter.Close(); err != nil {
				_ = batch.Close()
				return 0, 0, 0, fmt.Errorf("failed to close source iterator: %w", err)
			}

			if err := deleteChunk(pdb, sourceDB, destDB, deleteKeys, dbName, recordDeleted); err != nil {
				_ = batch.Close()
				return 0, 0, 0, err
			}
			totalDeleted += int64(len(deleteKeys))
			deleteKeys = deleteKeys[:0]
			bytesSinceDelete = 0

			var err error
			if srcIter, err = sourceDB.Iterator(lastKey, nil); err != nil {
				_ = batch.Close()
				return 0, 0, 0, fmt.Errorf("failed to reopen source iterator: %w", err)
			}
			if srcIter.Valid() && bytes.Equal(srcIter.Key(), lastKey) {
				srcIter.Next() // lastKey was just deleted; skip it if still present
			}
			continue
		}
		srcIter.Next()
	}

	if err := srcIter.Error(); err != nil {
		return fail(fmt.Errorf("iterator error: %w", err))
	}
	if err := srcIter.Close(); err != nil {
		_ = batch.Close()
		return 0, 0, 0, fmt.Errorf("failed to close source iterator: %w", err)
	}
	if err := batch.Write(); err != nil {
		_ = batch.Close()
		return 0, 0, 0, fmt.Errorf("failed to write final batch: %w", err)
	}
	_ = batch.Close()

	// Delete whatever remains in the final partial chunk.
	if len(deleteKeys) > 0 {
		if err := deleteChunk(pdb, sourceDB, destDB, deleteKeys, dbName, recordDeleted); err != nil {
			return 0, 0, 0, err
		}
		totalDeleted += int64(len(deleteKeys))
	}

	return totalKeys, totalBytes, totalDeleted, nil
}

// deleteChunk is the only place that removes source data. It durably flushes the
// destination, confirms every key in the chunk is present in the destination with
// the same value as the source, records the running count, and only then deletes
// the keys from the source and compacts the freed range. Returns without deleting
// anything if validation fails, so a key can never be lost from both databases.
func deleteChunk(pdb *db.PebbleDB, sourceDB, destDB db.DB, keys [][]byte, dbName string, recordDeleted func(int64) error) error {
	if len(keys) == 0 {
		return nil
	}
	// Force memtables to durable SSTs, then verify the chunk round-trips.
	if pdb != nil {
		if err := pdb.DB().Flush(); err != nil {
			return fmt.Errorf("[%s] flush before delete failed: %w", dbName, err)
		}
	}
	for _, key := range keys {
		srcVal, err := sourceDB.Get(key)
		if err != nil {
			return fmt.Errorf("[%s] validate: source read failed: %w", dbName, err)
		}
		destVal, err := destDB.Get(key)
		if err != nil {
			return fmt.Errorf("[%s] validate: dest read failed: %w", dbName, err)
		}
		if !bytes.Equal(srcVal, destVal) {
			return fmt.Errorf("[%s] validate: value mismatch before delete at key %x — refusing to delete source", dbName, key)
		}
	}

	// Persist the count BEFORE physically deleting from the source. At this point
	// the keys are confirmed durably present in the destination, so recording
	// first keeps the no-backup lower bound accurate even if a crash happens
	// between here and the delete: the keys are in dest, and on resume the
	// undeleted source keys (all <= dest's last key) are skipped and cleaned up by
	// the final source removal. Recording AFTER the delete could undercount.
	if recordDeleted != nil {
		if err := recordDeleted(int64(len(keys))); err != nil {
			return err
		}
	}
	if err := deleteSourceKeys(sourceDB, keys); err != nil {
		return fmt.Errorf("[%s] failed to delete source keys: %w", dbName, err)
	}
	if ldb, ok := sourceDB.(*db.GoLevelDB); ok {
		if err := ldb.ForceCompact(keys[0], keys[len(keys)-1]); err != nil {
			return fmt.Errorf("[%s] source compaction failed: %w", dbName, err)
		}
	}
	return nil
}

// deleteSourceKeys deletes the given keys from sourceDB in batches capped at maxDeleteBatch.
func deleteSourceKeys(sourceDB db.DB, keys [][]byte) error {
	batch := sourceDB.NewBatch()
	batchCount := 0
	for _, key := range keys {
		if err := batch.Delete(key); err != nil {
			_ = batch.Close()
			return err
		}
		batchCount++
		size, _ := batch.GetByteSize()
		if size >= maxDeleteBatch {
			if err := batch.WriteSync(); err != nil {
				_ = batch.Close()
				return err
			}
			_ = batch.Close()
			batch = sourceDB.NewBatch()
			batchCount = 0
		}
	}
	if batchCount > 0 {
		if err := batch.WriteSync(); err != nil {
			_ = batch.Close()
			return err
		}
	}
	return batch.Close()
}

// verifyDestination reopens the staged PebbleDB (which runs pebble's own
// checkConsistency) and fully iterates it, reading every value to surface any
// block-level corruption or missing SST. In --backup mode it additionally
// compares the full source against the destination (count + content hash).
func verifyDestination(t dbTarget, ds DBState, opts migrateOpts) error {
	// Reopening runs pebble's checkConsistency; the full read surfaces corruption.
	destCount, destHash, err := openAndFullRead(t.fileName, t.stagingDir())
	if err != nil {
		return fmt.Errorf("destination failed to open or read back (consistency check failed): %w", err)
	}

	if opts.backup {
		// Source still exists; do a full authoritative comparison.
		sourceDB, err := db.NewDB(t.fileName, db.GoLevelDBBackend, t.dir)
		if err != nil {
			return fmt.Errorf("failed to open source for verification: %w", err)
		}
		defer func() { _ = sourceDB.Close() }()
		srcCount, srcHash, err := iterateCountHash(sourceDB)
		if err != nil {
			return fmt.Errorf("source iteration failed: %w", err)
		}
		if srcCount != destCount {
			return fmt.Errorf("key count mismatch: source=%d dest=%d", srcCount, destCount)
		}
		if !bytes.Equal(srcHash, destHash) {
			return fmt.Errorf("content hash mismatch between source and destination")
		}
		fmt.Printf("[%s] Verified %d keys, source/dest content hash match\n", t.name, destCount)
		return nil
	}

	// No-backup mode: the source is gone, so we cannot reconstruct an exact
	// expected count (crash-resume can re-copy a few boundary keys). The data-loss
	// guarantee comes from the per-chunk validate-before-delete gate: every key
	// removed from the source was confirmed present in the destination first, so
	// the destination must contain at least every validated-and-deleted key. A
	// count below that lower bound indicates real loss; the full read-back above
	// covers corruption.
	if destCount < ds.SourceKeysDeleted {
		return fmt.Errorf("key count below validated lower bound: dest=%d < validated-deleted=%d — possible data loss", destCount, ds.SourceKeysDeleted)
	}
	fmt.Printf("[%s] Verified %d keys (full read), consistency check passed (>= %d validated source deletes)\n", t.name, destCount, ds.SourceKeysDeleted)
	return nil
}

// iterateCountHash iterates the entire DB in key order, reading every value, and
// returns the key count plus a SHA-256 over the length-prefixed key/value stream.
// openAndFullRead opens the PebbleDB at dir/<fileName>.db, fully iterates it
// (reading every value, which surfaces missing SSTs and block-level corruption),
// and returns the key count and a content hash. Opening also runs pebble's own
// checkConsistency.
func openAndFullRead(fileName, dir string) (int64, []byte, error) {
	d, err := db.NewDB(fileName, db.PebbleDBBackend, dir)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = d.Close() }()
	return iterateCountHash(d)
}

func iterateCountHash(d db.DB) (int64, []byte, error) {
	it, err := d.Iterator(nil, nil)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = it.Close() }()

	h := sha256.New()
	var count int64
	var lenBuf [8]byte
	for ; it.Valid(); it.Next() {
		k := it.Key()
		v := it.Value()
		writeLenPrefixed(h, lenBuf[:], k)
		writeLenPrefixed(h, lenBuf[:], v)
		count++
	}
	if err := it.Error(); err != nil {
		return 0, nil, err
	}
	return count, h.Sum(nil), nil
}

func writeLenPrefixed(h hash.Hash, lenBuf, b []byte) {
	binary.BigEndian.PutUint64(lenBuf, uint64(len(b)))
	_, _ = h.Write(lenBuf)
	_, _ = h.Write(b)
}

// runCheck opens each target's database (expected PebbleDB) at its final
// location and verifies it opens and fully iterates without error.
func runCheck(targets []dbTarget, backend string) error {
	fmt.Printf("Running consistency check (effective backend: %q)...\n", backend)
	var failures int
	for _, t := range targets {
		if _, err := os.Stat(t.srcPath()); os.IsNotExist(err) {
			if t.optional {
				fmt.Printf("[%s] not present, skipping\n", t.name)
				continue
			}
			fmt.Printf("[%s] MISSING at %s\n", t.name, t.srcPath())
			failures++
			continue
		}
		count, _, err := openAndFullRead(t.fileName, t.dir)
		if err != nil {
			fmt.Printf("[%s] FAILED: %v\n", t.name, err)
			failures++
			continue
		}
		fmt.Printf("[%s] OK (%d keys)\n", t.name, count)
	}
	if failures > 0 {
		return fmt.Errorf("consistency check failed for %d database(s)", failures)
	}
	fmt.Println("\nAll databases opened and fully iterated successfully.")
	return nil
}

// State file management

// loadState reads the migration state file, returning nil if it doesn't exist.
func loadState(stateDir string) (*MigrationState, error) {
	path := filepath.Join(stateDir, migrationStateFile)
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

// saveState atomically writes the migration state file via tmp+fsync+rename.
func saveState(state *MigrationState, stateDir string) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(stateDir, migrationStateFile), data, 0o644)
}

// atomicWriteFile writes data to path via a temp file + fsync + rename, then
// fsyncs the parent directory so the rename is durable.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return fsyncDir(dir)
}

// fsyncDir fsyncs a directory so that renames/creations within it are durable.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		// Some filesystems return EINVAL for directory fsync; tolerate that.
		return nil
	}
	return nil
}

// performAutoSwap moves staged PebbleDB databases into place using a crash-safe,
// stage-don't-destroy strategy, then updates the backend in both app.toml and
// config.toml. The old LevelDB directories are moved aside (not deleted) until
// the swap and config update succeed.
func performAutoSwap(homeDir string, targets []dbTarget, tracker *stateTracker, backup bool) error {
	// Gate: every database must be verified (or, only if optional, legitimately
	// absent). A required database that is missing must never let the swap flip
	// the node's config.
	for _, t := range targets {
		ds := tracker.getDBState(t.name)
		switch {
		case ds.Status == statusVerified || ds.Status == statusSwapped:
		case ds.Status == statusNotFound && t.optional:
		default:
			return fmt.Errorf("database %q has status %q, expected %q — cannot auto-swap", t.name, ds.Status, statusVerified)
		}
	}

	fmt.Println("\nPerforming auto-swap...")

	// Track moved-aside backups for rollback if the config update fails.
	type moved struct {
		target     dbTarget
		backupPath string // where the old LevelDB was moved (empty if none)
	}
	var movedList []moved

	rollback := func() {
		fmt.Fprintln(os.Stderr, "Rolling back swap...")
		for _, m := range slices.Backward(movedList) {
			// Move the new pebble DB back to staging.
			if _, err := os.Stat(m.target.srcPath()); err == nil {
				_ = os.Rename(m.target.srcPath(), m.target.stagedPath())
			}
			// Restore the original LevelDB.
			if m.backupPath != "" {
				_ = os.Rename(m.backupPath, m.target.srcPath())
			}
		}
	}

	for _, t := range targets {
		if tracker.getDBState(t.name).Status == statusNotFound {
			fmt.Printf("  [%s] skipping (not found)\n", t.name)
			continue
		}

		staged := t.stagedPath()
		if _, err := os.Stat(staged); err != nil {
			// Staged DB is gone. If the destination is already PebbleDB, this DB
			// was already swapped by an earlier (interrupted) run — idempotently
			// skip it so a crash mid-swap can be resumed.
			if isPebbleDB(t.srcPath()) {
				fmt.Printf("  [%s] already swapped, skipping\n", t.name)
				continue
			}
			rollback()
			return fmt.Errorf("[%s] expected staged database missing at %s and destination is not PebbleDB: %w", t.name, staged, err)
		}

		var backupPath string
		if _, err := os.Stat(t.srcPath()); err == nil {
			// Old LevelDB still present (backup mode). Move it aside (never delete here).
			backupPath = t.backupPath()
			_ = os.RemoveAll(backupPath)
			fmt.Printf("  [%s] preserving old DB -> %s\n", t.name, backupPath)
			if err := os.Rename(t.srcPath(), backupPath); err != nil {
				rollback()
				return fmt.Errorf("[%s] failed to move old DB aside: %w", t.name, err)
			}
		}

		fmt.Printf("  [%s] moving %s -> %s\n", t.name, staged, t.srcPath())
		if err := os.Rename(staged, t.srcPath()); err != nil {
			rollback()
			return fmt.Errorf("[%s] failed to move staged DB into place: %w", t.name, err)
		}
		if err := fsyncDir(t.dir); err != nil {
			rollback()
			return fmt.Errorf("[%s] failed to fsync %s: %w", t.name, t.dir, err)
		}

		ds := tracker.getDBState(t.name)
		ds.Status = statusSwapped
		_ = tracker.updateDBState(t.name, ds)

		movedList = append(movedList, moved{target: t, backupPath: backupPath})
	}

	// Final open check after all moves, before touching config.
	fmt.Println("  Verifying swapped databases open in place...")
	for _, t := range targets {
		if tracker.getDBState(t.name).Status != statusSwapped {
			continue
		}
		d, err := db.NewDB(t.fileName, db.PebbleDBBackend, t.dir)
		if err != nil {
			rollback()
			return fmt.Errorf("[%s] swapped database failed to open at %s: %w", t.name, t.srcPath(), err)
		}
		_ = d.Close()
	}

	// Update both backend settings. Config update is fatal with rollback.
	if err := updateBackendConfig(homeDir, pebbleBackendName); err != nil {
		rollback()
		return fmt.Errorf("failed to update backend config (rolled back swap): %w", err)
	}
	fmt.Printf("  Updated backend = %q in app.toml and config.toml\n", pebbleBackendName)

	// Success: handle the moved-aside LevelDB backups. Iterate all targets (not
	// just movedList) so this is correct even on a resumed swap where some
	// backups were created by an earlier run.
	for _, t := range targets {
		bak := t.backupPath()
		if _, err := os.Stat(bak); err != nil {
			continue
		}
		if backup {
			fmt.Printf("  [%s] backup preserved at %s\n", t.name, bak)
			continue
		}
		fmt.Printf("  [%s] removing old LevelDB %s\n", t.name, bak)
		if err := os.RemoveAll(bak); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to remove old DB %s: %v\n", bak, err)
		}
	}

	// Clean up (now-empty) staging dirs and state.
	for _, t := range targets {
		_ = os.RemoveAll(t.stagingDir())
	}
	stateDir := filepath.Join(homeDir, "data_pebble")
	_ = os.Remove(filepath.Join(stateDir, migrationStateFile))
	_ = os.Remove(filepath.Join(stateDir, migrationLockFile))
	_ = os.Remove(stateDir)

	fmt.Println("\nAuto-swap complete. Start your node to verify.")
	return nil
}

// loadCometConfig loads the CometBFT configuration from config.toml.
func loadCometConfig(homeDir string) (*cmtcfg.Config, error) {
	cfg := cmtcfg.DefaultConfig()
	cfg.SetRoot(homeDir)

	configPath := filepath.Join(homeDir, "config", "config.toml")
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("toml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return cfg, nil
}

// effectiveBackend returns the backend the node will actually use for the app
// DB, following the SDK precedence: app.toml app-db-backend, then config.toml
// db_backend, then the goleveldb default.
func effectiveBackend(homeDir string) (string, error) {
	if v, ok, err := readTomlString(filepath.Join(homeDir, "config", "app.toml"), appDBBackendKey); err != nil {
		return "", err
	} else if ok && v != "" {
		return v, nil
	}
	if v, ok, err := readTomlString(filepath.Join(homeDir, "config", "config.toml"), configDBBackendKey); err != nil {
		return "", err
	} else if ok && v != "" {
		return v, nil
	}
	return levelDBBackendName, nil
}

// readTomlString reads a top-level `key = "value"` entry from a TOML file.
// Returns (value, found, error). A missing file returns ("", false, nil).
func readTomlString(path, key string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, v, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"`), true, nil
		}
	}
	return "", false, nil
}

// updateBackendConfig sets the backend in config.toml (db_backend) and, if it
// exists, app.toml (app-db-backend). It is transactional: if the app.toml write
// fails, config.toml is restored to its original contents so the two files never
// end up disagreeing (which would leave the app and consensus layers on
// different backends).
func updateBackendConfig(homeDir, backend string) error {
	configPath := filepath.Join(homeDir, "config", "config.toml")
	appPath := filepath.Join(homeDir, "config", "app.toml")

	origConfig, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config.toml: %w", err)
	}
	appExists := false
	if _, err := os.Stat(appPath); err == nil {
		appExists = true
	}

	if err := setTomlKey(configPath, configDBBackendKey, backend, true); err != nil {
		return fmt.Errorf("config.toml: %w", err)
	}
	if appExists {
		// app-db-backend takes precedence at runtime; it must agree with config.
		if err := setTomlKey(appPath, appDBBackendKey, backend, true); err != nil {
			// Restore config.toml so the two files stay consistent.
			if rerr := atomicWriteFile(configPath, origConfig, 0o644); rerr != nil {
				return fmt.Errorf("app.toml: %w (and failed to restore config.toml: %v)", err, rerr)
			}
			return fmt.Errorf("app.toml: %w (config.toml restored)", err)
		}
	}
	return nil
}

// setTomlKey surgically replaces (or appends, if addIfMissing) a top-level
// `key = "value"` line, preserving all other content, then writes atomically.
func setTomlKey(path, key, value string, addIfMissing bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, _, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(k) == key {
			lines[i] = fmt.Sprintf(`%s = "%s"`, key, value)
			found = true
			break
		}
	}
	if !found {
		if !addIfMissing {
			return fmt.Errorf("%q setting not found in %s", key, path)
		}
		lines = append([]string{fmt.Sprintf(`%s = "%s"`, key, value)}, lines...)
	}
	return atomicWriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// printNextSteps prints post-migration manual-swap instructions.
func printNextSteps(targets []dbTarget, backup bool, tracker *stateTracker) {
	var mvCommands strings.Builder
	for _, t := range targets {
		if tracker.getDBState(t.name).Status == statusNotFound {
			continue
		}
		if backup {
			_, _ = fmt.Fprintf(&mvCommands, "   mv %s %s\n", t.srcPath(), t.backupPath())
		}
		_, _ = fmt.Fprintf(&mvCommands, "   mv %s %s\n", t.stagedPath(), t.srcPath())
	}

	fmt.Printf(`
Migration completed and verified successfully!

============================================================
Next Steps (manual swap):
============================================================

1. Move the migrated databases into place:
%s
2. Update the backend in BOTH files:
   config/config.toml:  db_backend = "pebbledb"
   config/app.toml:     app-db-backend = "pebbledb"

3. Start your node and verify it is running properly.

============================================================
`, mvCommands.String())
}

// isPebbleDB checks whether a database directory is a PebbleDB by looking for
// OPTIONS-* files, which are created by PebbleDB but not by LevelDB.
func isPebbleDB(dbPath string) bool {
	matches, err := filepath.Glob(filepath.Join(dbPath, "OPTIONS-*"))
	return err == nil && len(matches) > 0
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
