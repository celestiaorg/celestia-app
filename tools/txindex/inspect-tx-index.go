package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	dbm "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/types"
	"github.com/cosmos/gogoproto/proto"
)

const (
	tagKeySeparator   = "/"
	eventSeqSeparator = "$es$"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run inspect-tx-index.go <path-to-tx_index.db>")
		fmt.Println("Example: go run inspect-tx-index.go ./tx_index.db")
		os.Exit(1)
	}

	dbPath := os.Args[1]
	if !filepath.IsAbs(dbPath) {
		cwd, _ := os.Getwd()
		dbPath = filepath.Join(cwd, dbPath)
	}

	// Open the database
	db, err := dbm.NewGoLevelDB("tx_index", filepath.Dir(dbPath))
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	defer db.Close()

	fmt.Printf("Inspecting database at: %s\n", dbPath)
	fmt.Println(strings.Repeat("=", 50))

	// Get all keys
	itr, err := db.Iterator(nil, nil)
	if err != nil {
		log.Fatalf("Error creating iterator: %v", err)
	}
	defer itr.Close()

	keyCount := 0
	txResultCount := 0
	eventKeys := make([]string, 0)
	heightKeys := make([]string, 0)
	hashKeys := make([]string, 0)

	for ; itr.Valid(); itr.Next() {
		key := string(itr.Key())
		keyCount++

		// Categorize keys
		if isHashKey(itr.Key()) {
			hashKeys = append(hashKeys, key)
		} else if isHeightKey(key) {
			heightKeys = append(heightKeys, key)
		} else if isEventKey(key) {
			eventKeys = append(eventKeys, key)
		}

		// Try to parse as TxResult if it looks like a hash key
		if isHashKey(itr.Key()) {
			txResult := new(abci.TxResult)
			if err := proto.Unmarshal(itr.Value(), txResult); err == nil {
				txResultCount++
			}
		}

		// Show first 10 keys for each category
		if keyCount <= 10 {
			fmt.Printf("Key: %s\n", key)
			fmt.Printf("  Value (hex): %x\n", itr.Value())
			fmt.Printf("  Value (len): %d\n", len(itr.Value()))
			if isHashKey(itr.Key()) {
				// Try to parse as TxResult
				txResult := new(abci.TxResult)
				if err := proto.Unmarshal(itr.Value(), txResult); err == nil {
					fmt.Printf("  TxResult: Height=%d, Index=%d, Code=%d\n",
						txResult.Height, txResult.Index, txResult.Result.Code)
				}
			}
			fmt.Println()
		}
	}

	fmt.Printf("Database Statistics:\n")
	fmt.Printf("  Total keys: %d\n", keyCount)
	fmt.Printf("  Transaction results: %d\n", txResultCount)
	fmt.Printf("  Hash keys: %d\n", len(hashKeys))
	fmt.Printf("  Height keys: %d\n", len(heightKeys))
	fmt.Printf("  Event keys: %d\n", len(eventKeys))

	// Show some sample keys from each category
	fmt.Println("\nSample Hash Keys:")
	for i, key := range hashKeys {
		if i >= 5 {
			break
		}
		fmt.Printf("  %s\n", key)
	}

	fmt.Println("\nSample Height Keys:")
	for i, key := range heightKeys {
		if i >= 5 {
			break
		}
		fmt.Printf("  %s\n", key)
	}

	fmt.Println("\nSample Event Keys:")
	for i, key := range eventKeys {
		if i >= 5 {
			break
		}
		fmt.Printf("  %s\n", key)
	}

	// Analyze event types
	eventTypes := make(map[string]int)
	for _, key := range eventKeys {
		eventType := extractEventType(key)
		if eventType != "" {
			eventTypes[eventType]++
		}
	}

	fmt.Println("\nEvent Types Found:")
	for eventType, count := range eventTypes {
		fmt.Printf("  %s: %d occurrences\n", eventType, count)
	}
}

func isHashKey(key []byte) bool {
	// Hash keys are typically 32 bytes (sha256) and don't contain separators
	return len(key) == 32 && !strings.Contains(string(key), tagKeySeparator)
}

func isHeightKey(key string) bool {
	return strings.HasPrefix(key, types.TxHeightKey+tagKeySeparator)
}

func isEventKey(key string) bool {
	// Event keys contain separators and are not height keys
	return strings.Contains(key, tagKeySeparator) && !isHeightKey(key) && !isHashKey([]byte(key))
}

func extractEventType(key string) string {
	// Event keys are in format: eventType.attributeKey/attributeValue/height/txIndex/eventSeq
	parts := strings.Split(key, tagKeySeparator)
	if len(parts) >= 1 {
		typeParts := strings.Split(parts[0], ".")
		if len(typeParts) >= 1 {
			return typeParts[0]
		}
	}
	return ""
}
