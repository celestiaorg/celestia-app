package main

import (
	"fmt"
	"log"
	"time"

	trace "github.com/celestiaorg/celestia-app/tools/ntrace/pkg"
)

func main() {
	if err := trace.Generate("output", trainingData()); err != nil {
		log.Fatalf("failed to trace: %v", err)
	}
	fmt.Println("done")
}

func trainingData() trace.Messages {
	return []trace.Message{
		{
			ID:        "vote1",
			Node:      "node1",
			Type:      "vote",
			Timestamp: timeStamp(1 * time.Second),
		},
		{
			ID:        "vote1",
			Node:      "node2",
			Type:      "vote",
			Timestamp: timeStamp(3 * time.Second),
		},
		{
			ID:        "vote2",
			Node:      "node3",
			Type:      "vote",
			Timestamp: timeStamp(4 * time.Second),
		},
		{
			ID:        "vote2",
			Node:      "node1",
			Type:      "vote",
			Timestamp: timeStamp(6 * time.Second),
		},
		{
			ID:        "block1",
			Node:      "node1",
			Type:      "block",
			Timestamp: timeStamp(2 * time.Second),
		},
		{
			ID:        "block1",
			Node:      "node2",
			Type:      "block",
			Timestamp: timeStamp(8 * time.Second),
		},
	}
}

func timeStamp(delta time.Duration) time.Time {
	return time.Unix(16711234400, 0).Add(delta)
}
