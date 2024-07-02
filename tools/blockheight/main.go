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

	currentHeight := 5627
	currentTime := time.Now().Truncate(time.Second)
	targetTime := time.Date(2024, 7, 4, 12, 30, 0, 0, location).Truncate(time.Second) // July 4th, 2024 @ 12:30PM ET

	diffInSeconds := targetTime.Sub(currentTime).Seconds()
	diffInBlockHeight := math.Floor(diffInSeconds / blockTime)
	targetHeight := currentHeight + int(diffInBlockHeight)

	fmt.Printf("currentHeight: %v\n", currentHeight)
	fmt.Printf("currentTime: %v\n", currentTime.String())
	fmt.Printf("targetTime: %v\n", targetTime.String())
	fmt.Printf("diffInSeconds: %v\n", diffInSeconds)
	fmt.Printf("diffInBlockHeight: %v\n", diffInBlockHeight)
	fmt.Printf("targetHeight: %v\n", targetHeight)
}
