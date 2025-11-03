package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

const (
	defaultHeight  = 1034505
	defaultTimeout = 10 * time.Second
)

var (
	height    int64
	timeout   time.Duration
	endpoints []string
)

// Default list of archive RPC endpoints
var defaultEndpoints = []string{
	// From https://celestia-tools.brightlystake.com/rpc-status
	"https://celestia-rpc.brightlystake.com",
	"https://rpc.celestia.pops.one",
	"https://celestia.archive.rpc.stakewith.us",
	"https://celestia-rpc.lavenderfive.com",
	"https://celestia.rpc.kjnodes.com",
	"https://celestia-mainnet-rpc.autostake.com",
	"https://rpc-celestia.whispernode.com",
	"https://celestia-rpc.easy2stake.com",
	"https://rpc-celestia.cosmos-spaces.cloud",
	"https://celestia-rpc.openbitlab.com",

	// From https://itrocket.net/services/mainnet/celestia/public-rpc/
	"https://celestia-mainnet-rpc.itrocket.net",

	// Additional known endpoints
	"https://public-celestia-rpc.numia.xyz",
	"https://celestia-rpc.mesa-nodes.com",
	"https://celestia-archive-rpc.rpc-archive.stakewith.us",
	"https://rpc-celestia-archive.trusted-point.com",
}

type checkResult struct {
	endpoint string
	success  bool
	latency  time.Duration
	error    string
	height   int64
}

type blockResultsResponse struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  struct {
		Height string `json:"height"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    string `json:"data"`
	} `json:"error"`
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rpc-checker",
		Short: "Check block_results availability across multiple archive RPC providers",
		Long: `A tool for checking if specific block_results are available across multiple 
Celestia archive RPC providers. This is useful for debugging data availability issues.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(endpoints) == 0 {
				endpoints = defaultEndpoints
			}
			return checkEndpoints(height, timeout, endpoints)
		},
	}

	cmd.Flags().Int64VarP(&height, "height", "b", defaultHeight, "Block height to query")
	cmd.Flags().DurationVarP(&timeout, "timeout", "t", defaultTimeout, "Timeout for each request")
	cmd.Flags().StringSliceVarP(&endpoints, "endpoints", "e", []string{}, "Custom list of RPC endpoints (comma-separated)")

	return cmd
}

func checkEndpoints(height int64, timeout time.Duration, endpoints []string) error {
	fmt.Printf("Checking block_results at height %d across %d RPC providers\n", height, len(endpoints))
	fmt.Printf("Request timeout: %s\n\n", timeout)

	// Since requests run concurrently, we only need a small buffer beyond the timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout+5*time.Second)
	defer cancel()

	results := make([]checkResult, len(endpoints))
	var wg sync.WaitGroup

	for i, endpoint := range endpoints {
		wg.Add(1)
		go func(idx int, ep string) {
			defer wg.Done()
			results[idx] = checkEndpoint(ctx, ep, height, timeout)
		}(i, endpoint)
	}

	wg.Wait()

	// Sort results by endpoint name for consistent output
	sort.Slice(results, func(i, j int) bool {
		return results[i].endpoint < results[j].endpoint
	})

	printResults(results)
	return nil
}

func checkEndpoint(ctx context.Context, endpoint string, height int64, timeout time.Duration) checkResult {
	result := checkResult{
		endpoint: endpoint,
	}

	url := fmt.Sprintf("%s/block_results?height=%d", endpoint, height)

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
	if err != nil {
		result.error = fmt.Sprintf("failed to create request: %v", err)
		return result
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: timeout,
	}

	start := time.Now()
	resp, err := client.Do(req)
	result.latency = time.Since(start)

	if err != nil {
		result.error = fmt.Sprintf("request failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.error = fmt.Sprintf("failed to read response: %v", err)
		return result
	}

	if resp.StatusCode != http.StatusOK {
		result.error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
		return result
	}

	var blockResp blockResultsResponse
	if err := json.Unmarshal(body, &blockResp); err != nil {
		result.error = fmt.Sprintf("failed to parse JSON: %v", err)
		return result
	}

	if blockResp.Error != nil {
		result.error = fmt.Sprintf("RPC error %d: %s", blockResp.Error.Code, blockResp.Error.Message)
		if blockResp.Error.Data != "" {
			result.error += fmt.Sprintf(" (data: %s)", blockResp.Error.Data)
		}
		return result
	}

	result.success = true
	result.height = height
	return result
}

func printResults(results []checkResult) {
	successCount := 0
	for _, r := range results {
		if r.success {
			successCount++
		}
	}

	fmt.Printf("Results: %d/%d endpoints succeeded\n\n", successCount, len(results))

	// Print table header
	fmt.Printf("%-60s | %-10s | %-10s | %s\n", "Endpoint", "Status", "Latency", "Error")
	for i := 0; i < 140; i++ {
		fmt.Printf("-")
	}
	fmt.Println()

	// Print results
	for _, r := range results {
		status := "✗ FAILED"
		if r.success {
			status = "✓ SUCCESS"
		}

		latencyStr := "-"
		if r.latency > 0 {
			latencyStr = fmt.Sprintf("%dms", r.latency.Milliseconds())
		}

		errorStr := "-"
		if r.error != "" {
			// Truncate long error messages
			if len(r.error) > 50 {
				errorStr = r.error[:47] + "..."
			} else {
				errorStr = r.error
			}
		}

		fmt.Printf("%-60s | %-10s | %-10s | %s\n", r.endpoint, status, latencyStr, errorStr)
	}

	fmt.Println()
	fmt.Printf("Summary:\n")
	fmt.Printf("  Successful: %d (%.1f%%)\n", successCount, float64(successCount)/float64(len(results))*100)
	fmt.Printf("  Failed:     %d (%.1f%%)\n", len(results)-successCount, float64(len(results)-successCount)/float64(len(results))*100)
}
