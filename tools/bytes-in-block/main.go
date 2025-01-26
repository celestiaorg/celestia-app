package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// Stats holds the stats portion of each block
type Stats struct {
	BytesInBlock int64 `json:"bytes_in_block"`
	BlockTime    int64 `json:"block_time"`
}

// Block represents the entire block from the JSON array
type Block struct {
	ID     int   `json:"id"`
	Height int   `json:"height"`
	Stats  Stats `json:"stats"`
}

func main() {
	startHeight := 4170798
	endHeight := 4170898
	numberOfBlocksPerRequest := 100

	var totalBlocks int64
	var sumBytes int64
	var sumBlockTime int64

	// We'll loop offset in increments of `limit`.
	// For each iteration, we do a GET request with the given offset + limit=100
	// until we've covered the range [start, end).
	for offset := startHeight; offset <= endHeight; offset += numberOfBlocksPerRequest {
		// Use the min of `offset+limit-1` or `end` if you want to ensure
		// you don't exceed the last block. But the endpoint might just return fewer
		// blocks if you overshoot. In practice, `offset` is just how many items to skip.
		// We'll keep it simple:
		url := fmt.Sprintf("https://api-mocha.celenium.io/v1/block?limit=%d&offset=%d&sort=asc&stats=true", numberOfBlocksPerRequest, offset-startHeight)

		resp, err := http.Get(url)
		if err != nil {
			log.Fatalf("Failed to GET %s: %v", url, err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Fatalf("Failed to read response body: %v", err)
		}

		var blocks []Block
		err = json.Unmarshal(body, &blocks)
		if err != nil {
			log.Fatalf("JSON unmarshal error (offset=%d): %v", offset, err)
		}

		// If we got zero blocks back, there's no more data to fetch.
		if len(blocks) == 0 {
			fmt.Printf("No more blocks returned at offset %d. Stopping.\n", offset)
			break
		}

		// Accumulate sums
		for _, b := range blocks {
			// If the block height is beyond `end`, we can optionally skip it:
			if b.Height > endHeight {
				break
			}
			sumBytes += b.Stats.BytesInBlock
			sumBlockTime += b.Stats.BlockTime
		}

		totalBlocks += int64(len(blocks))

		// Optional: if the last block we received has a height >= end, we can break.
		lastHeight := blocks[len(blocks)-1].Height
		if lastHeight >= endHeight {
			break
		}
	}

	if totalBlocks == 0 {
		fmt.Println("No blocks returned overall.")
		return
	}

	// Calculate overall averages
	avgBytes := float64(sumBytes) / float64(totalBlocks)
	avgBlockTimeMs := float64(sumBlockTime) / float64(totalBlocks)

	// Convert bytes to MiB
	avgBytesMiB := avgBytes / (1024.0 * 1024.0)
	// Convert milliseconds to seconds
	avgBlockTimeSec := avgBlockTimeMs / 1000.0

	fmt.Printf("Fetched a total of %d blocks (from ~%d up to ~%d).\n", totalBlocks, startHeight, endHeight)
	fmt.Printf("Average bytes_in_block: %.2f bytes (~%.2f MiB)\n", avgBytes, avgBytesMiB)
	fmt.Printf("Average block_time:     %.2f ms (~%.2f seconds)\n", avgBlockTimeMs, avgBlockTimeSec)
}
