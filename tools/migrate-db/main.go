package main

import (
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
	backup := flag.Bool("backup", true, "Create backup of LevelDB databases before migration")
	cleanup := flag.Bool("cleanup", false, "Remove old LevelDB files after successful migration (not recommended)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Migrate celestia-app databases from LevelDB to PebbleDB.\n\n")
		fmt.Fprintf(os.Stderr, "This tool will:\n")
		fmt.Fprintf(os.Stderr, "1. Create a new 'data_pebble' directory in your home folder\n")
		fmt.Fprintf(os.Stderr, "2. Create backups of existing LevelDB databases\n")
		fmt.Fprintf(os.Stderr, "3. Migrate all databases to PebbleDB format in 'data_pebble'\n")
		fmt.Fprintf(os.Stderr, "4. After migration, you can move the databases to the 'data' directory\n\n")
		fmt.Fprintf(os.Stderr, "Databases migrated:\n")
		fmt.Fprintf(os.Stderr, "- application.db (Application state)\n")
		fmt.Fprintf(os.Stderr, "- blockstore.db (Block storage)\n")
		fmt.Fprintf(os.Stderr, "- state.db (Consensus state)\n")
		fmt.Fprintf(os.Stderr, "- tx_index.db (Transaction index)\n")
		fmt.Fprintf(os.Stderr, "- evidence.db (Evidence storage)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Dry-run to test\n")
		fmt.Fprintf(os.Stderr, "  %s --dry-run\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Actual migration\n")
		fmt.Fprintf(os.Stderr, "  %s\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Migration with custom home directory\n")
		fmt.Fprintf(os.Stderr, "  %s --home /custom/path/.celestia-app\n", os.Args[0])
	}

	flag.Parse()

	if err := migrateDB(*homeDir, *dryRun, *cleanup, *backup); err != nil {
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
	// Note: cs.wal, priv_validator_state.json, snapshots, and traces are not databases
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

		// Create backup if requested
		if backup {
			if err := createBackup(levelDBPath); err != nil {
				return fmt.Errorf("failed to create backup for %s: %w", dbName, err)
			}
		}

		// Perform migration
		if err := migrateSingleDB(dbName, dataDir, pebbleDataDir); err != nil {
			return fmt.Errorf("failed to migrate %s: %w", dbName, err)
		}

		fmt.Printf("Successfully migrated %s.db\n\n", dbName)
	}

	if dryRun {
		fmt.Println("Dry-run complete. No changes were made.")
		return nil
	}

	fmt.Println("Migration completed successfully!")
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("NEXT STEPS:")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("\n1. STOP your node if it's running:")
	fmt.Println("   sudo systemctl stop celestia-appd")
	fmt.Println("\n2. UPDATE config.toml to use PebbleDB:")
	fmt.Println("   nano ~/.celestia-app/config/config.toml")
	fmt.Println("   Change:")
	fmt.Println("   [db]")
	fmt.Println("   backend = \"pebbledb\"")
	fmt.Println("\n3. MOVE the migrated databases:")
	fmt.Println("   # Backup originals first (if not already done)")
	if !backup {
		for _, dbName := range databases {
			fmt.Printf("   mv %s/%s.db %s/%s.db.backup\n", dataDir, dbName, dataDir, dbName)
		}
	}
	fmt.Println("   # Move PebbleDB files")
	for _, dbName := range databases {
		fmt.Printf("   mv %s/%s.db %s/%s.db\n", pebbleDataDir, dbName, dataDir, dbName)
	}
	fmt.Println("\n4. START your node:")
	fmt.Println("   sudo systemctl start celestia-appd")
	fmt.Println("\n5. VERIFY it's working:")
	fmt.Println("   celestia-appd status")
	fmt.Println("   journalctl -u celestia-appd -f")
	fmt.Println("\n6. CLEANUP after verifying (optional):")
	fmt.Printf("   rm -rf %s\n", pebbleDataDir)
	if backup {
		fmt.Println("   rm -rf " + dataDir + "/*.db.leveldb.backup")
	}
	fmt.Println("\n" + strings.Repeat("=", 60))

	return nil
}

func createBackup(dbPath string) error {
	backupPath := dbPath + ".leveldb.backup"

	fmt.Printf("Creating backup: %s -> %s\n", dbPath, backupPath)

	// Check if backup already exists
	if _, err := os.Stat(backupPath); err == nil {
		fmt.Printf("Backup already exists at %s, skipping\n", backupPath)
		return nil
	}

	// Copy directory recursively
	if err := copyDir(dbPath, backupPath); err != nil {
		return err
	}

	fmt.Printf("Backup created successfully\n")
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

	// Open destination PebbleDB
	// db.NewDB will create: destDir/dbName.db/
	fmt.Printf("Creating PebbleDB in %s...\n", destDir)
	destDB, err := db.NewDB(dbName, db.PebbleDBBackend, destDir)
	if err != nil {
		return fmt.Errorf("failed to create destination PebbleDB: %w", err)
	}

	// Migrate data
	fmt.Printf("Migrating data...\n")
	count := 0
	totalBytes := int64(0)

	iter, err := sourceDB.Iterator(nil, nil)
	if err != nil {
		destDB.Close()
		return fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	batch := destDB.NewBatch()
	batchSize := 0
	const maxBatchSize = 1000

	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()

		if err := batch.Set(key, value); err != nil {
			batch.Close()
			destDB.Close()
			return fmt.Errorf("failed to set key in batch: %w", err)
		}

		count++
		batchSize++
		totalBytes += int64(len(key) + len(value))

		// Commit batch periodically
		if batchSize >= maxBatchSize {
			if err := batch.WriteSync(); err != nil {
				batch.Close()
				destDB.Close()
				return fmt.Errorf("failed to write batch: %w", err)
			}
			batch.Close()
			batch = destDB.NewBatch()
			batchSize = 0

			if count%10000 == 0 {
				fmt.Printf("Migrated %d keys...\n", count)
			}
		}
	}

	// Write final batch
	if batchSize > 0 {
		if err := batch.WriteSync(); err != nil {
			batch.Close()
			destDB.Close()
			return fmt.Errorf("failed to write final batch: %w", err)
		}
	}
	batch.Close()

	if err := iter.Error(); err != nil {
		destDB.Close()
		return fmt.Errorf("iterator error: %w", err)
	}

	// Close source DB first
	if err := sourceDB.Close(); err != nil {
		fmt.Printf("WARNING: Failed to close source DB: %v\n", err)
	}

	// Close destination DB properly
	fmt.Printf("Closing PebbleDB...\n")
	if err := destDB.Close(); err != nil {
		return fmt.Errorf("failed to close destination DB: %w", err)
	}

	// Verify the database can be reopened
	fmt.Printf("Verifying PebbleDB integrity...\n")
	testDB, err := db.NewDB(dbName, db.PebbleDBBackend, destDir)
	if err != nil {
		return fmt.Errorf("failed to reopen PebbleDB for verification: %w", err)
	}

	// Try to create an iterator to verify the database is readable
	testIter, err := testDB.Iterator(nil, nil)
	if err != nil {
		testDB.Close()
		return fmt.Errorf("failed to create iterator on new PebbleDB: %w", err)
	}

	// Count keys to verify
	verifyCount := 0
	for ; testIter.Valid(); testIter.Next() {
		verifyCount++
		if verifyCount%10000 == 0 {
			fmt.Printf("Verified %d keys...\n", verifyCount)
		}
	}
	testIter.Close()

	if err := testIter.Error(); err != nil {
		testDB.Close()
		return fmt.Errorf("iterator error during verification: %w", err)
	}

	testDB.Close()

	if verifyCount != count {
		return fmt.Errorf("verification failed: expected %d keys, found %d keys", count, verifyCount)
	}

	duration := time.Since(startTime)
	fmt.Printf("Migrated and verified %d keys (%d bytes) in %s\n", count, totalBytes, duration)
	fmt.Printf("Successfully created PebbleDB at %s/%s.db\n", destDir, dbName)

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
