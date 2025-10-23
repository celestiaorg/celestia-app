package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cosmos/cosmos-db"
)

func main() {
	homeDir := flag.String("home", os.ExpandEnv("$HOME/.celestia-app"), "Node home directory")
	dryRun := flag.Bool("dry-run", false, "Run migration in dry-run mode without making changes")
	noBackup := flag.Bool("no-backup", false, "Skip creating backup of data directory before migration")
	cleanup := flag.Bool("cleanup", false, "Remove old LevelDB files after successful migration (not recommended)")

	flag.Usage = func() {
		usage := `Usage: migrate-db [options]

Migrate celestia-app databases from LevelDB to PebbleDB.

This tool will:
1. Create a backup of the entire data directory (unless --no-backup is specified)
2. Create a new 'data_pebble' directory in your celestia-app home folder
3. Migrate all databases to PebbleDB format in 'data_pebble'
4. After migration, you can move the databases to the 'data' directory using the provided commands

Databases migrated:
- application.db (Application state)
- blockstore.db (Block storage)
- state.db (Consensus state)
- tx_index.db (Transaction index)
- evidence.db (Evidence storage)

Options:
`
		fmt.Fprintf(os.Stderr, usage)
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
		fmt.Fprintf(os.Stderr, examples)
	}

	flag.Parse()

	if err := migrateDB(*homeDir, *dryRun, *cleanup, !*noBackup); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func migrateDB(homeDir string, dryRun, cleanup, backup bool) error {
	dataDir := filepath.Join(homeDir, "data")
	pebbleDataDir := filepath.Join(homeDir, "data_pebble")

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

	fmt.Printf("Starting database migration from LevelDB to PebbleDB\n")
	fmt.Printf("Home directory: %s\n", homeDir)
	fmt.Printf("Source directory (LevelDB): %s\n", dataDir)
	fmt.Printf("Destination directory (PebbleDB): %s\n", pebbleDataDir)
	fmt.Printf("Dry-run mode: %v\n", dryRun)
	fmt.Printf("Cleanup old files: %v\n", cleanup)
	fmt.Printf("Create backups: %v\n\n", backup)

	// Ask for confirmation before proceeding (unless in dry-run mode)
	if !dryRun {
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
	if backup && !dryRun {
		backupDir := filepath.Join(homeDir, "data_backup")
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

	// Create data_pebble directory
	if !dryRun {
		if _, err := os.Stat(pebbleDataDir); err == nil {
			return fmt.Errorf("destination directory already exists: %s\nPlease remove it or move it before running migration", pebbleDataDir)
		}
		if err := os.MkdirAll(pebbleDataDir, 0755); err != nil {
			return fmt.Errorf("failed to create pebble data directory: %w", err)
		}
		fmt.Printf("Created destination directory: %s\n\n", pebbleDataDir)
	}

	for _, dbName := range databases {
		fmt.Printf("=== Migrating %s.db ===\n", dbName)

		// Check if LevelDB exists
		levelDBPath := filepath.Join(dataDir, dbName+".db")
		if _, err := os.Stat(levelDBPath); os.IsNotExist(err) {
			fmt.Printf("WARNING: LevelDB not found at %s, skipping\n\n", levelDBPath)
			continue
		}

		if dryRun {
			fmt.Printf("DRY-RUN: Would migrate %s to %s/%s.db\n\n", levelDBPath, pebbleDataDir, dbName)
			continue
		}

		// Perform migration
		if err := migrateSingleDB(dbName, dataDir, pebbleDataDir); err != nil {
			return fmt.Errorf("failed to migrate %s: %w", dbName, err)
		}

		// Verify the migrated database
		if err := verifyDB(dbName, pebbleDataDir); err != nil {
			return fmt.Errorf("failed to verify %s: %w", dbName, err)
		}

		fmt.Printf("Successfully migrated %s.db\n\n", dbName)
	}

	if dryRun {
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
		fmt.Fprintf(&mvCommands, "   mv %s/%s.db %s/%s.db\n", pebbleDataDir, dbName, dataDir, dbName)
	}

	// Build cleanup commands
	cleanupCommands := fmt.Sprintf("   rm -rf %s\n", pebbleDataDir)
	if backup {
		backupDir := filepath.Join(homeDir, "data_backup")
		cleanupCommands += fmt.Sprintf("   rm -rf %s\n", backupDir)
	}

	nextSteps := `
Migration completed successfully!

============================================================
NEXT STEPS:
============================================================

1. UPDATE config.toml to use PebbleDB:
   [db]
   backend = "pebbledb"

2. MOVE the migrated databases:
   # Remove old databases
%s
   # Move PebbleDB files
%s
3. Start your node and verify that it is running properly

4. CLEANUP after verifying (optional):
%s
============================================================
`
	fmt.Printf(nextSteps, rmCommands.String(), mvCommands.String(), cleanupCommands)

	return nil
}

func migrateSingleDB(dbName, sourceDir, destDir string) error {
	startTime := time.Now()

	// Open source LevelDB
	fmt.Printf("Opening LevelDB from %s...\n", sourceDir)
	sourceDB, err := db.NewDB(dbName, db.GoLevelDBBackend, sourceDir)
	if err != nil {
		return fmt.Errorf("failed to open source LevelDB: %w", err)
	}
	defer func(sourceDB db.DB) {
		err := sourceDB.Close()
		if err != nil {
			fmt.Println("failed to close source DB: %w", err)
		}
	}(sourceDB)

	// Open destination PebbleDB
	// db.NewDB will create: destDir/dbName.db/
	fmt.Printf("Creating PebbleDB in %s...\n", destDir)
	destDB, err := db.NewDB(dbName, db.PebbleDBBackend, destDir)
	if err != nil {
		return fmt.Errorf("failed to create destination PebbleDB: %w", err)
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
		return fmt.Errorf("failed to create iterator: %w", err)
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
			return fmt.Errorf("failed to set key in batch: %w", err)
		}

		count++
		batchSize++
		totalBytes += int64(len(key) + len(value))

		// Commit batch periodically
		if batchSize >= maxBatchSize {
			if err := batch.WriteSync(); err != nil {
				batch.Close()
				return fmt.Errorf("failed to write batch: %w", err)
			}
			err := batch.Close()
			if err != nil {
				return err
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
			return fmt.Errorf("failed to write final batch: %w", err)
		}
	}

	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterator error: %w", err)
	}

	duration := time.Since(startTime)
	fmt.Printf("Migrated %d keys (%d bytes) in %s\n", count, totalBytes, duration)

	return nil
}

func verifyDB(dbName, destDir string) error {
	fmt.Printf("Verifying PebbleDB integrity...\n")

	testDB, err := db.NewDB(dbName, db.PebbleDBBackend, destDir)
	if err != nil {
		return fmt.Errorf("failed to open PebbleDB for verification: %w", err)
	}
	defer func(testDB db.DB) {
		err := testDB.Close()
		if err != nil {
			fmt.Println("failed to close verification DB: %w", err)
		}
	}(testDB)

	// Try to create an iterator to verify the database is readable
	testIter, err := testDB.Iterator(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create iterator on PebbleDB: %w", err)
	}
	defer func(testIter db.Iterator) {
		err := testIter.Close()
		if err != nil {
			fmt.Println("failed to close verification iterator: %w", err)
		}
	}(testIter)

	// Count keys to verify
	verifyCount := 0
	for ; testIter.Valid(); testIter.Next() {
		verifyCount++
		if verifyCount%10000 == 0 {
			fmt.Printf("Verified %d keys...\n", verifyCount)
		}
	}

	if err := testIter.Error(); err != nil {
		return fmt.Errorf("iterator error during verification: %w", err)
	}

	fmt.Printf("Verified %d keys successfully\n", verifyCount)
	return nil
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
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

	return os.WriteFile(dst, data, 0644)
}
