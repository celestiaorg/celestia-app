package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type LogEntry struct {
	ChainID   string `json:"chain_id"`
	NodeID    string `json:"node_id"`
	Table     string `json:"table"`
	Timestamp string `json:"timestamp"`
	Msg       struct {
		Height      int    `json:"height"`
		Round       int    `json:"round"`
		Latency     int64  `json:"latency"`
		MessageType string `json:"message_type"`
	} `json:"msg"`
}

type Stats struct {
	Count   int
	Min     float64
	Max     float64
	Average float64
	Median  float64
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: signing_latency <file_path>")
		os.Exit(1)
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Printf("error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	latencies := make(map[string][]float64)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		var entry LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		msgType := entry.Msg.MessageType
		latencyMs := float64(entry.Msg.Latency) / 1_000_000.0
		latencies[msgType] = append(latencies[msgType], latencyMs)
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("error reading file: %v\n", err)
		os.Exit(1)
	}

	for msgType, values := range latencies {
		if len(values) == 0 {
			continue
		}

		stats := calculateStats(values)
		fmt.Printf("\n%s:\n", msgType)
		fmt.Printf("  count:   %d\n", stats.Count)
		fmt.Printf("  min:     %.2f ms\n", stats.Min)
		fmt.Printf("  max:     %.2f ms\n", stats.Max)
		fmt.Printf("  average: %.2f ms\n", stats.Average)
		fmt.Printf("  median:  %.2f ms\n", stats.Median)
	}
}

func calculateStats(values []float64) Stats {
	if len(values) == 0 {
		return Stats{}
	}

	sort.Float64s(values)

	sum := 0.0
	for _, v := range values {
		sum += v
	}

	median := values[len(values)/2]
	if len(values)%2 == 0 {
		median = (values[len(values)/2-1] + values[len(values)/2]) / 2.0
	}

	return Stats{
		Count:   len(values),
		Min:     values[0],
		Max:     values[len(values)-1],
		Average: sum / float64(len(values)),
		Median:  median,
	}
}
