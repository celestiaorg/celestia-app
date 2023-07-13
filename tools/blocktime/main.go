package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/tendermint/tendermint/rpc/client/http"
)

func main() {
	if err := Run(); err != nil {
		fmt.Printf("ERROR: %s", err.Error())
	}
}

func Run() error {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <node_rpc> [query_range]\n", os.Args[0])
		return nil
	}

	url := os.Args[1]
	c, err := http.New(url, "/websocket")
	if err != nil {
		return err
	}
	resp, err := c.Status(context.Background())
	if err != nil {
		return err
	}
	lastHeight := resp.SyncInfo.LatestBlockHeight
	chainID := resp.NodeInfo.Network
	queryRange := 100
	if len(os.Args) == 3 {
		queryRange, err = strconv.Atoi(os.Args[2])
		if err != nil {
			return err
		}
	}
	blockTimes := make([]time.Time, 0, queryRange)
	for i := lastHeight - int64(queryRange); i < lastHeight; i++ {
		if i < 2 {
			continue
		}
		resp, err := c.Commit(context.Background(), &i)
		if err != nil {
			return err
		}

		blockTimes = append(blockTimes, resp.Header.Time)
	}
	avgTime, minTime, maxTime, stdvTime := analyzeBlockTimes(blockTimes)
	fmt.Printf(`
Chain: %s
Block Time (from %d to %d):
	Average: %.2fs
	Min: %.2fs
	Max: %.2fs
	Standard Deviation: %.3fs

`, chainID,
		lastHeight-int64(queryRange),
		lastHeight,
		avgTime/1000,
		minTime/1000,
		maxTime/1000,
		stdvTime/1000,
	)
	return nil
}

func analyzeBlockTimes(times []time.Time) (float64, float64, float64, float64) {
	totalTime := times[len(times)-1].Sub(times[0])
	averageTime := float64(totalTime.Milliseconds()) / float64(len(times)-1)
	variance, minTime, maxTime := float64(0), float64(0), float64(0)
	for i := 1; i < len(times); i++ {
		diff := float64(times[i].Sub(times[i-1]).Milliseconds())
		if minTime == 0 || diff < minTime {
			minTime = diff
		}
		if maxTime == 0 || diff > maxTime {
			maxTime = diff
		}
		variance += math.Pow(averageTime-diff, 2)
	}
	stddev := math.Sqrt(variance / float64(len(times)-1))
	return averageTime, minTime, maxTime, stddev
}
