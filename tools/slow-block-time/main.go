package main

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/cometbft/cometbft/rpc/client/http"
)

func main() {
	if err := Run(); err != nil {
		fmt.Printf("ERROR: %s", err.Error())
	}
}

const (
	url        = "https://celestia-mocha-rpc.publicnode.com:443"
	queryRange = 100
)

func Run() error {
	c, err := http.New(url, "/websocket")
	if err != nil {
		return err
	}
	resp, err := c.Status(context.Background())
	if err != nil {
		return err
	}
	chainID := resp.NodeInfo.Network
	fmt.Printf("Chain ID: %s\n", chainID)

	lastHeight := resp.SyncInfo.LatestBlockHeight
	firstHeight := max(lastHeight-int64(queryRange)+1, 1)

	for {
		blockTimes, err := queryBlockTimes(c, firstHeight, lastHeight)
		if err != nil {
			return err
		}
		printInfo(blockTimes, firstHeight, lastHeight)
		lastHeight = firstHeight - 1
		firstHeight = firstHeight - int64(queryRange)
	}
}

func queryBlockTimes(c *http.HTTP, firstHeight, lastHeight int64) ([]time.Time, error) {
	blockTimes := make([]time.Time, 0, lastHeight-firstHeight+1)
	for height := firstHeight; height <= lastHeight; height++ {
		resp, err := c.Commit(context.Background(), &height)
		if err != nil {
			return nil, err
		}
		blockTimes = append(blockTimes, resp.Time)
	}
	return blockTimes, nil
}

func printInfo(blockTimes []time.Time, firstHeight, lastHeight int64) {
	_, _, maxTime, _ := analyzeBlockTimes(blockTimes)
	fmt.Printf("Block %v to %v, Max: %.2fs\n", firstHeight, lastHeight, maxTime/1000)
}

// analyzeBlockTimes returns the average, min, max, and standard deviation of the block times.
// Units are in milliseconds.
func analyzeBlockTimes(times []time.Time) (float64, float64, float64, float64) {
	numberOfObservations := len(times) - 1
	totalTime := times[numberOfObservations].Sub(times[0])
	averageTime := float64(totalTime.Milliseconds()) / float64(numberOfObservations)
	variance, minTime, maxTime := float64(0), float64(0), float64(0)
	for i := 0; i < numberOfObservations; i++ {
		diff := float64(times[i+1].Sub(times[i]).Milliseconds())
		if minTime == 0 || diff < minTime {
			minTime = diff
		}
		if maxTime == 0 || diff > maxTime {
			maxTime = diff
		}
		variance += (averageTime - diff) * (averageTime - diff)
	}
	stddev := math.Sqrt(variance / float64(numberOfObservations))
	return averageTime, minTime, maxTime, stddev
}
