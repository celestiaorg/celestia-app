package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/cometbft/cometbft/rpc/client/http"
)

// --- CONSTANTS ---

const (
	// DefaultBlockTime is the observed average time between blocks (Celestia Mainnet Beta).
	// Ideally, this value should be passed as a command-line flag for flexibility.
	DefaultBlockTime = 11.75 

	// Standard RPC endpoints for examples
	ExampleMainnetRPC = "https://celestia-rpc.publicnode.com:443"
	ExampleMainnetTime = "2024-09-18T14:00:00"

	// Layout is the expected time format for targetTime (ISO 8601 subset).
	Layout = "2006-01-02T15:04:05"
	
	// DefaultRPCEndpoint is the standard HTTP endpoint path for CometBFT RPC
	DefaultRPCEndpoint = "/"
)

func main() {
	// Standard Go practice: print error to Stderr and exit with status 1
	if err := Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

// Run executes the core logic for block height prediction.
func Run() error {
	// Check for required arguments (RPC URL and Target Time)
	if len(os.Args) < 3 {
		fmt.Printf("Usage: %s <node_rpc_url> <target_time>\n", os.Args[0])
		fmt.Printf("Example: %s %s %s\n", os.Args[0], ExampleMainnetRPC, ExampleMainnetTime)
		return nil
	}

	// Use slice indexing for clean argument extraction, skipping os.Args[0] (program name)
	nodeRPC := os.Args[1]
	targetTimeArg := os.Args[2]

	// 1. Initialize CometBFT RPC client
	// Use DefaultRPCEndpoint ("/") instead of "/websocket" for the HTTP client.
	c, err := http.New(nodeRPC, DefaultRPCEndpoint)
	if err != nil {
		return fmt.Errorf("failed to create RPC client: %w", err)
	}

	// 2. Fetch current chain status
	resp, err := c.Status(context.Background())
	if err != nil {
		return fmt.Errorf("failed to fetch node status: %w", err)
	}

	// Extract current chain data
	chainID := resp.NodeInfo.Network
	currentHeight := resp.SyncInfo.LatestBlockHeight
	currentTime := resp.SyncInfo.LatestBlockTime

	// 3. Parse target time
	targetTime, err := time.Parse(Layout, targetTimeArg)
	if err != nil {
		return fmt.Errorf("error parsing target time '%s': %w", targetTimeArg, err)
	}

	// 4. Validation
	if currentTime.After(targetTime) {
		return fmt.Errorf("current time %v is already after target time %v", currentTime, targetTime)
	}

	// 5. Prediction Calculation
	diffInSeconds := targetTime.Sub(currentTime).Seconds()
	
	// Calculate the difference in block height. Use DefaultBlockTime constant.
	// math.Floor is necessary here to ensure an integer block count.
	diffInBlockHeight := math.Floor(diffInSeconds / DefaultBlockTime)
	targetHeight := currentHeight + int64(diffInBlockHeight)

	// 6. Output Results
	fmt.Printf("--- Block Height Prediction ---\n")
	fmt.Printf("Chain ID: %v\n", chainID)
	fmt.Printf("Current Height: %v\n", currentHeight)
	fmt.Printf("Current Time: %v\n", currentTime.Format(time.RFC3339))
	fmt.Printf("Target Time: %v\n", targetTime.Format(time.RFC3339))
	fmt.Printf("Predicted Target Height: %v\n", targetHeight)
	fmt.Printf("--- Calculation Details (using %v s/block) ---\n", DefaultBlockTime)
	// Outputting only the integer part of seconds for cleaner display
	fmt.Printf("Time Difference (seconds): %v\n", int64(diffInSeconds)) 
	fmt.Printf("Predicted Height Difference (blocks): %v\n", int64(diffInBlockHeight))
	
	return nil
}
