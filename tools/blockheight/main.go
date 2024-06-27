package main

import (
	"fmt"
	"math"
	"time"
)

// blockTime is the observed average time between blocks. You can update this
// value based on the block time on https://www.mintscan.io/celestia/block/. The
// accuracy of the block height prediction depends on the accuracy of this
// value.
var blockTime = 11.86 // seconds between blocks

func main() {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}

	now := time.Now().Truncate(time.Second)
	target := time.Date(2024, 7, 3, 12, 0, 0, 0, location).Truncate(time.Second)
	diffInSeconds := target.Sub(now).Seconds()
	diffInBlockHeight := math.Floor(diffInSeconds / blockTime)

	fmt.Printf("Now: %v\n", now.String())
	fmt.Printf("Target: %v\n", target.String())
	fmt.Printf("Diff in seconds: %v\n", diffInSeconds)
	fmt.Printf("Diff in block heights: %v\n", diffInBlockHeight)
}
