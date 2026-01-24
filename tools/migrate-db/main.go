package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	db "github.com/cosmos/cosmos-db"
)

//  opts.FormatMajorVersion = pebble2.FormatNewest
// 	opts.Experimental.ValueSeparationPolicy = func() pebble2.ValueSeparationPolicy {
//		return pebble2.ValueSeparationPolicy{
//			Enabled:               true,
//			MinimumSize:           1024,
//			MaxBlobReferenceDepth: 10,
//			RewriteMinimumAge:     5 * time.Minute,
//          GarbageRatioLowPriority : 0.10
//          GarbageRatioHighPriority : 0.20
//		}
//	}

type options struct {
	homeDir       string
	sourceBackend string
	targetBackend string
	dryRun        bool
	backup        bool
}

func main() {
	homeDir := flag.String("home", os.ExpandEnv("$HOME/.celestia-app"), "Node home directory")
	dryRun := flag.Bool("dry-run", false, "Run migration in dry-run mode without making changes")
	noBackup := flag.Bool("no-backup", false, "Skip creating backup of data directory before migration")
	sourceBackend := flag.String("source-backend", "leveldb", "Backend used in existing databases")
	targetBackend := flag.String("target-backend", "pebble2", "Target backend for migration")

	flag.Usage = func() {
		usage := `Usage: migrate-db [options]

Migrate celestia-app databases from one backend to another (for example from LevelDB to PebbleDB v2).

This tool will:
1. Create a backup of the entire data directory (unless --no-backup is specified)
2. Create a new 'data_<backend>' directory in your celestia-app home folder
3. Migrate all databases to new backend format in 'data_<backend>'
4. After migration, you can move the databases to the 'data' directory using the provided commands

Supported backends: "goleveldb", "rocksdb", "pebbledb", "pebbledb2"

Databases migrated:
- application.db (Application state)
- blockstore.db (Block storage)
- state.db (Consensus state)
- tx_index.db (Transaction index)
- evidence.db (Evidence storage)

Options:
`
		fmt.Fprintf(os.Stderr, "%s", usage)
		flag.PrintDefaults()
		examples := `
Examples:
  # Dry-run to test
  migrate-db --dry-run

  # Actual migration (with backup)
  migrate-db

  # Migration without backup
  migrate-db --no-backup

  # Migration with custom home directory
  migrate-db --home /custom/path/.celestia-app
`
		fmt.Fprintf(os.Stderr, "%s", examples)
	}

	flag.Parse()

	opts := options{
		homeDir:       *homeDir,
		sourceBackend: *sourceBackend,
		targetBackend: *targetBackend,
		dryRun:        *dryRun,
		backup:        !*noBackup,
	}

	if err := migrateDB(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func migrateDB(opts options) error {
	dataDir := filepath.Join(opts.homeDir, "data")
	targetDataDir := filepath.Join(opts.homeDir, "data_"+opts.targetBackend)

	// Verify data directory exists
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		return fmt.Errorf("data directory does not exist: %s", dataDir)
	}

	// Database names to migrate
	databases := []string{
		"application",
		"blockstore",
		"state",
		"tx_index",
		"evidence",
	}

	fmt.Printf("Starting database migration from %s to %s\n", opts.sourceBackend, opts.targetBackend)
	fmt.Printf("Home directory: %s\n", opts.homeDir)
	fmt.Printf("Source directory (%s): %s\n", opts.sourceBackend, dataDir)
	fmt.Printf("Destination directory (%s): %s\n", opts.targetBackend, targetDataDir)
	fmt.Printf("Dry-run mode: %v\n", opts.dryRun)
	fmt.Printf("Create backups: %v\n\n", opts.backup)

	// Ask for confirmation before proceeding (unless in dry-run mode)
	if !opts.dryRun {
		fmt.Print("Do you want to continue with the migration? (y/n): ")
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read user input: %w", err)
		}
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "yes" && response != "y" {
			fmt.Println("Migration cancelled by user.")
			return nil
		}
		fmt.Println()
	}

	// Create backup of entire data directory if requested
	if opts.backup && !opts.dryRun {
		backupDir := filepath.Join(opts.homeDir, "data_backup")
		if _, err := os.Stat(backupDir); err == nil {
			return fmt.Errorf("backup directory already exists: %s\nPlease remove it or move it before running migration", backupDir)
		}
		fmt.Printf("Creating backup of data directory...\n")
		fmt.Printf("Backup: %s -> %s\n", dataDir, backupDir)
		if err := copyDir(dataDir, backupDir); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
		fmt.Printf("Backup created successfully\n\n")
	}

	// Create data_<targetBackend> directory
	if !opts.dryRun {
		if _, err := os.Stat(targetDataDir); err == nil {
			return fmt.Errorf("destination directory already exists: %s\nPlease remove it or move it before running migration", targetDataDir)
		}
		if err := os.MkdirAll(targetDataDir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s data directory: %w", opts.targetBackend, err)
		}
		fmt.Printf("Created destination directory: %s\n\n", targetDataDir)
	}

	for _, dbName := range databases {
		fmt.Printf("=== Migrating %s.db ===\n", dbName)

		// Check if source exists
		sourcePath := filepath.Join(dataDir, dbName+".db")
		if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
			fmt.Printf("Warning: %s not found at %s, skipping\n\n", opts.sourceBackend, sourcePath)
			continue
		}

		if opts.dryRun {
			fmt.Printf("Dry-run: Would migrate %s to %s/%s.db\n\n", sourcePath, targetDataDir, dbName)
			continue
		}

		// Perform migration
		migratedCount, err := migrateSingleDB(dbName, dataDir, targetDataDir, opts)
		if err != nil {
			return fmt.Errorf("failed to migrate %s: %w", dbName, err)
		}

		// Verify the migrated database
		if err := verifyDB(dbName, targetDataDir, migratedCount); err != nil {
			return fmt.Errorf("failed to verify %s: %w", dbName, err)
		}

		fmt.Printf("Successfully migrated %s.db\n\n", dbName)
	}

	if opts.dryRun {
		fmt.Println("Dry-run complete. No changes were made.")
		return nil
	}

	// Build the removal commands
	var rmCommands strings.Builder
	for _, dbName := range databases {
		fmt.Fprintf(&rmCommands, "   rm -rf %s/%s.db\n", dataDir, dbName)
	}

	// Build the move commands
	var mvCommands strings.Builder
	for _, dbName := range databases {
		fmt.Fprintf(&mvCommands, "   mv %s/%s.db %s/%s.db\n", targetDataDir, dbName, dataDir, dbName)
	}

	// Build cleanup commands
	cleanupCommands := fmt.Sprintf("   rm -rf %s\n", targetDataDir)
	if opts.backup {
		backupDir := filepath.Join(opts.homeDir, "data_backup")
		cleanupCommands += fmt.Sprintf("   rm -rf %s\n", backupDir)
	}

	nextSteps := `
Migration completed successfully!

============================================================
Next Steps:
============================================================

1. Update config.toml to use %s:
   [db]
   backend = "%s"

2. Move the migrated databases:
   # Remove old databases
%s
   # Move new files
%s
3. Start your node and verify that it is running properly

4. Cleanup after verifying (optional):
%s
============================================================
`
	fmt.Printf(nextSteps, opts.targetBackend, opts.targetBackend, rmCommands.String(), mvCommands.String(), cleanupCommands)

	return nil
}

func migrateSingleDB(dbName, sourceDir, destDir string, opts options) (int, error) {
	startTime := time.Now()

	// Open source database
	fmt.Printf("Opening %s from %s...\n", opts.sourceBackend, sourceDir)
	sourceDB, err := db.NewDB(dbName, db.BackendType(opts.sourceBackend), sourceDir)
	if err != nil {
		return 0, fmt.Errorf("failed to open source %s: %w", opts.sourceBackend, err)
	}
	defer func(sourceDB db.DB) {
		err := sourceDB.Close()
		if err != nil {
			fmt.Println("failed to close source DB: %w", err)
		}
	}(sourceDB)

	// Open destination database
	// db.NewDB will create: destDir/dbName.db/
	fmt.Printf("Creating %s in %s...\n", opts.targetBackend, destDir)
	destDB, err := db.NewDB(dbName, db.BackendType(opts.targetBackend), destDir)
	if err != nil {
		return 0, fmt.Errorf("failed to create destination %s: %w", opts.targetBackend, err)
	}
	defer func(destDB db.DB) {
		err := destDB.Close()
		if err != nil {
			if err != nil {
				fmt.Println("failed to close destination DB: %w", err)
			}
		}
	}(destDB)

	// Migrate data
	fmt.Printf("Migrating data...\n")
	count := 0
	totalBytes := int64(0)

	iter, err := sourceDB.Iterator(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer func(iter db.Iterator) {
		err := iter.Close()
		if err != nil {
			if err != nil {
				fmt.Println("failed to close iterator: %w", err)
			}
		}
	}(iter)

	batch := destDB.NewBatch()
	batchSize := 0
	const maxBatchSize = 1000

	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()

		if err := batch.Set(key, value); err != nil {
			batch.Close()
			return 0, fmt.Errorf("failed to set key in batch: %w", err)
		}

		count++
		batchSize++
		totalBytes += int64(len(key) + len(value))

		// Commit batch periodically
		if batchSize >= maxBatchSize {
			if err := batch.WriteSync(); err != nil {
				batch.Close()
				return 0, fmt.Errorf("failed to write batch: %w", err)
			}
			err := batch.Close()
			if err != nil {
				return 0, err
			}
			batch = destDB.NewBatch()
			batchSize = 0

			if count%10000 == 0 {
				fmt.Printf("Migrated %d keys...\n", count)
			}
		}
	}

	// Write the final batch
	if batchSize > 0 {
		if err := batch.WriteSync(); err != nil {
			batch.Close()
			return 0, fmt.Errorf("failed to write final batch: %w", err)
		}
	}

	if err := iter.Error(); err != nil {
		return 0, fmt.Errorf("iterator error: %w", err)
	}

	duration := time.Since(startTime)
	fmt.Printf("Migrated %d keys (%d bytes) in %s\n", count, totalBytes, duration)

	return count, nil
}

func verifyDB(dbName, destDir string, expectedCount int, opts options) error {
	fmt.Printf("Verifying %s integrity...\n", dbName)

	// Open destination database
	destDB, err := db.NewDB(dbName, db.BackendType(opts.targetBackend), destDir)
	if err != nil {
		return fmt.Errorf("failed to open %s for verification: %w", opts.targetBackend, err)
	}
	defer func(destDB db.DB) {
		err := destDB.Close()
		if err != nil {
			fmt.Println("failed to close verification DB: %w", err)
		}
	}(destDB)

	// Count keys in destination DB
	destIter, err := destDB.Iterator(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create iterator on %s: %w", opts.targetBackend, err)
	}
	defer func(destIter db.Iterator) {
		err := destIter.Close()
		if err != nil {
			fmt.Println("failed to close verification iterator: %w", err)
		}
	}(destIter)

	actualCount := 0
	for ; destIter.Valid(); destIter.Next() {
		actualCount++
		if actualCount%10000 == 0 {
			fmt.Printf("Verified %d keys...\n", actualCount)
		}
	}

	if err := destIter.Error(); err != nil {
		return fmt.Errorf("iterator error during verification: %w", err)
	}

	if actualCount != expectedCount {
		return fmt.Errorf("verification failed: expected %d keys, found %d keys", expectedCount, actualCount)
	}

	fmt.Printf("Verified %d keys successfully - count matches!\n", actualCount)
	return nil
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, 0o644)
}
