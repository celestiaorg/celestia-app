package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	dbm "github.com/cometbft/cometbft-db"

	"github.com/cometbft/cometbft/store"
)

var (
	dbTypeFlag = flag.String("db-type", "goleveldb", "database backend type (goleveldb, pebbledb, etc.)")
	dataDir    = flag.String("data-dir", "", "path to the data directory containing blockstore.db")
	checkHeight = flag.Int64("check-height", 0, "specific height to check for block parts (0 = check base height)")
	verbose     = flag.Bool("verbose", false, "show detailed information")
)

func main() {
	flag.Parse()

	if *dataDir == "" {
		// Try default location
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get home directory: %v", err)
		}
		*dataDir = filepath.Join(home, ".celestia-app", "data")
	}

	// Normalize path
	dataDirPath := strings.TrimPrefix(*dataDir, "~/")
	if dataDirPath != *dataDir {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get home directory: %v", err)
		}
		dataDirPath = filepath.Join(home, dataDirPath)
	}

	// Check if directory exists
	if _, err := os.Stat(dataDirPath); os.IsNotExist(err) {
		log.Fatalf("Data directory does not exist: %s", dataDirPath)
	}

	fmt.Printf("Analyzing blockstore at: %s\n", dataDirPath)
	fmt.Printf("Database type: %s\n\n", *dbTypeFlag)

	// Open blockstore database
	dbType := dbm.BackendType(*dbTypeFlag)
	blockStoreDB, err := dbm.NewDB("blockstore", dbType, dataDirPath)
	if err != nil {
		log.Fatalf("Failed to open blockstore database: %v", err)
	}
	defer blockStoreDB.Close()

	blockStore := store.NewBlockStore(blockStoreDB)
	defer blockStore.Close()

	// Get blockstore statistics
	base := blockStore.Base()
	height := blockStore.Height()
	size := blockStore.Size()

	fmt.Println("=== Blockstore Statistics ===")
	fmt.Printf("Base height:   %d\n", base)
	fmt.Printf("Current height: %d\n", height)
	fmt.Printf("Number of blocks: %d\n", size)

	if base == 0 && height == 0 {
		fmt.Println("\n⚠️  Blockstore is empty!")
		return
	}

	if base > 0 {
		fmt.Printf("\n✅ Pruning detected! Base height is %d (not 0)\n", base)
		fmt.Printf("   This means blocks below height %d have been pruned.\n", base)
	} else {
		fmt.Println("\n⚠️  No pruning detected (base = 0, all blocks from genesis are retained)")
	}

	// Check specific heights
	fmt.Println("\n=== Block Part Verification ===")

	// Check base height
	if base > 0 {
		fmt.Printf("\nChecking base height %d (oldest retained block):\n", base)
		checkBlockParts(blockStore, base, *verbose)
	}

	// Check a height below base (should be pruned)
	if base > 1 {
		checkHeight := base - 1
		fmt.Printf("\nChecking height %d (should be pruned, below base):\n", checkHeight)
		checkBlockParts(blockStore, checkHeight, *verbose)
	}

	// Check current height
	if height > 0 {
		fmt.Printf("\nChecking current height %d (latest block):\n", height)
		checkBlockParts(blockStore, height, *verbose)
	}

	// Check user-specified height
	if *checkHeight > 0 {
		fmt.Printf("\nChecking user-specified height %d:\n", *checkHeight)
		checkBlockParts(blockStore, *checkHeight, *verbose)
	}

	// Sample check: verify a few heights in the middle
	if size > 10 {
		sampleHeight := base + (size / 2)
		fmt.Printf("\nChecking sample height %d (middle of range):\n", sampleHeight)
		checkBlockParts(blockStore, sampleHeight, *verbose)
	}

	// Count block parts in database
	fmt.Println("\n=== Block Part Count Analysis ===")
	countBlockParts(blockStoreDB, base, height, *verbose)
}

func checkBlockParts(bs *store.BlockStore, height int64, verbose bool) {
	// Try to load block meta
	meta := bs.LoadBlockMeta(height)
	if meta == nil {
		fmt.Printf("  ❌ Block meta NOT found (block was pruned or doesn't exist)\n")
		return
	}

	fmt.Printf("  ✅ Block meta found\n")
	if verbose {
		fmt.Printf("    Block size: %d bytes\n", meta.BlockSize)
		fmt.Printf("    Number of transactions: %d\n", meta.NumTxs)
		fmt.Printf("    Expected parts: %d\n", meta.BlockID.PartSetHeader.Total)
	}

	// Try to load block parts
	partsFound := 0
	partsMissing := 0
	totalParts := int(meta.BlockID.PartSetHeader.Total)

	if totalParts == 0 {
		fmt.Printf("  ⚠️  Block has no parts (empty block)\n")
		return
	}

	// Check first, middle, and last parts
	checkIndices := []int{0}
	if totalParts > 1 {
		checkIndices = append(checkIndices, totalParts-1)
	}
	if totalParts > 2 {
		checkIndices = append(checkIndices, totalParts/2)
	}

	for _, idx := range checkIndices {
		part := bs.LoadBlockPart(height, idx)
		if part != nil {
			partsFound++
			if verbose {
				fmt.Printf("    ✅ Part %d/%d found (%d bytes)\n", idx+1, totalParts, len(part.Bytes))
			}
		} else {
			partsMissing++
			if verbose {
				fmt.Printf("    ❌ Part %d/%d MISSING (pruned)\n", idx+1, totalParts)
			}
		}
	}

	if partsMissing > 0 {
		fmt.Printf("  ❌ Block parts MISSING (block was partially or fully pruned)\n")
		fmt.Printf("    Found: %d/%d checked parts\n", partsFound, len(checkIndices))
	} else {
		fmt.Printf("  ✅ Block parts found (block is retained)\n")
		if !verbose {
			fmt.Printf("    Checked %d sample parts, all present\n", len(checkIndices))
		}
	}

	// Try to load full block
	block := bs.LoadBlock(height)
	if block != nil {
		fmt.Printf("  ✅ Full block can be loaded\n")
	} else {
		fmt.Printf("  ⚠️  Full block cannot be loaded (some parts may be missing)\n")
	}
}

func countBlockParts(db dbm.DB, base, height int64, verbose bool) {
	// Iterate through database to count block part keys
	// Block part keys have format: P:{height}:{partIndex}

	partCounts := make(map[int64]int)
	totalParts := 0

	iter, err := db.Iterator(nil, nil)
	if err != nil {
		fmt.Printf("⚠️  Could not iterate database: %v\n", err)
		return
	}
	defer iter.Close()

	for iter.Valid() {
		key := iter.Key()
		keyStr := string(key)

		// Check if this is a block part key (format: P:{height}:{index})
		if strings.HasPrefix(keyStr, "P:") {
			// Parse height from key
			// Format: P:{height}:{index}
			parts := strings.Split(keyStr, ":")
			if len(parts) >= 2 {
				var h int64
				if _, err := fmt.Sscanf(parts[1], "%d", &h); err == nil {
					partCounts[h]++
					totalParts++
				}
			}
		}

		iter.Next()
	}

	if err := iter.Error(); err != nil {
		fmt.Printf("⚠️  Error iterating database: %v\n", err)
		return
	}

	fmt.Printf("Total block parts found in database: %d\n", totalParts)

	if base > 0 {
		expectedMinHeight := base
		expectedMaxHeight := height

		// Count parts by height range
		partsInRange := 0
		partsBelowBase := 0

		for h, count := range partCounts {
			if h >= expectedMinHeight && h <= expectedMaxHeight {
				partsInRange += count
			} else if h < expectedMinHeight {
				partsBelowBase += count
			}
		}

		fmt.Printf("\nParts in expected range [%d, %d]: %d\n", expectedMinHeight, expectedMaxHeight, partsInRange)
		if partsBelowBase > 0 {
			fmt.Printf("⚠️  Parts below base height (%d): %d (should be 0 if pruning worked)\n", base, partsBelowBase)
		} else {
			fmt.Printf("✅ No parts found below base height (pruning successful)\n")
		}
	}

	if verbose && len(partCounts) > 0 {
		fmt.Println("\nParts per height (sample):")
		count := 0
		for h := base; h <= height && count < 10; h++ {
			if parts, ok := partCounts[h]; ok {
				fmt.Printf("  Height %d: %d parts\n", h, parts)
				count++
			}
		}
		if len(partCounts) > 10 {
			fmt.Printf("  ... (showing first 10 heights)\n")
		}
	}
}
