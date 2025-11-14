package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cometbft/cometbft/types"
)

func main() {
	if err := Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
		os.Exit(1)
	}
}

// getAllValidators fetches all validators at a given height, handling pagination
func getAllValidators(ctx context.Context, client *http.HTTP, height int64) ([]*types.Validator, error) {
	var allValidators []*types.Validator
	page := 1
	perPage := 100

	for {
		validatorsRes, err := client.Validators(ctx, &height, &page, &perPage)
		if err != nil {
			return nil, fmt.Errorf("failed to query validators at height %d page %d: %w", height, page, err)
		}

		allValidators = append(allValidators, validatorsRes.Validators...)

		if len(allValidators) >= validatorsRes.Total {
			break
		}
		page++
	}

	return allValidators, nil
}

// createValidatorSet creates a ValidatorSet from validators with their proposer priorities
func createValidatorSet(validators []*types.Validator) *types.ValidatorSet {
	if len(validators) == 0 {
		return &types.ValidatorSet{}
	}

	// Copy validators
	valsCopy := make([]*types.Validator, len(validators))
	for i, val := range validators {
		valsCopy[i] = val.Copy()
	}

	// Sort by voting power (descending), then by address (ascending)
	sort.Sort(types.ValidatorsByVotingPower(valsCopy))

	// Find proposer (validator with highest proposer priority)
	var proposer *types.Validator
	for _, val := range valsCopy {
		if proposer == nil || val.ProposerPriority > proposer.ProposerPriority {
			proposer = val
		} else if val.ProposerPriority == proposer.ProposerPriority {
			// Tie-breaker: lower address wins
			if bytes.Compare(val.Address, proposer.Address) < 0 {
				proposer = val
			}
		}
	}

	return &types.ValidatorSet{
		Validators: valsCopy,
		Proposer:   proposer,
	}
}

func Run() error {
	if len(os.Args) < 4 {
		fmt.Printf("Usage: %s <node_rpc> <start_height> <end_height>\n", os.Args[0])
		fmt.Printf("Example: %s http://localhost:26657 1 100\n", os.Args[0])
		return nil
	}

	url := os.Args[1]
	startHeight, err := strconv.ParseInt(os.Args[2], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid start height: %w", err)
	}
	endHeight, err := strconv.ParseInt(os.Args[3], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid end height: %w", err)
	}

	if startHeight > endHeight {
		return fmt.Errorf("start height must be less than or equal to end height")
	}
	if startHeight < 1 {
		return fmt.Errorf("start height must be at least 1")
	}

	// Create RPC client
	client, err := http.New(url, "/websocket")
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	if err := client.Start(); err != nil {
		return fmt.Errorf("failed to start client: %w", err)
	}
	defer client.Stop()

	ctx := context.Background()

	fmt.Printf("Verifying proposers from height %d to %d...\n\n", startHeight, endHeight)

	mismatches := 0
	totalBlocks := 0

	for height := startHeight; height <= endHeight; height++ {
		totalBlocks++

		// Get the current block
		blockRes, err := client.Block(ctx, &height)
		if err != nil {
			return fmt.Errorf("failed to query block at height %d: %w", height, err)
		}
		nextHeight := height + 1
		nextBlockRes, err := client.Block(ctx, &nextHeight)
		if err != nil {
			return fmt.Errorf("failed to query block at height %d: %w", nextHeight, err)
		}

		actualProposerAddress := blockRes.Block.ProposerAddress

		// Get round from LastCommit (this tells us how many rounds the PREVIOUS block took)
		var round int32
		if blockRes.Block.LastCommit != nil {
			round = blockRes.Block.LastCommit.Round
		}

		// Get validator set from the PREVIOUS block (height - 1)
		prevHeight := height - 1
		validators, err := getAllValidators(ctx, client, prevHeight)
		if err != nil {
			return err
		}

		// Create validator set from the previous block's state
		valSet := createValidatorSet(validators)

		// Increment proposer priority (round + 1) times
		// This accounts for all rounds at the previous height plus the move to current height
		times := nextBlockRes.Block.LastCommit.Round + 1
		valSet.IncrementProposerPriority(times)

		// Get expected proposer
		expectedProposer := valSet.GetProposer()
		if expectedProposer == nil {
			return fmt.Errorf("failed to get proposer at height %d", height)
		}

		// Compare
		match := bytes.Equal(actualProposerAddress, expectedProposer.Address)

		status := "✓ MATCH"
		if !match {
			status = "✗ MISMATCH"
			mismatches++
		}

		fmt.Printf("Height %d: %s", height, status)
		if round > 0 {
			fmt.Printf(" (round %d)", round)
		}
		fmt.Println()
		fmt.Printf("  Actual:   %X\n", actualProposerAddress)
		fmt.Printf("  Expected: %X\n", expectedProposer.Address)
		if !match {
			fmt.Println()
		}
	}

	// Print summary
	fmt.Printf("\n=== SUMMARY ===\n")
	fmt.Printf("Total blocks checked: %d\n", totalBlocks)
	fmt.Printf("Proposer matches: %d\n", totalBlocks-mismatches)
	fmt.Printf("Proposer mismatches: %d\n", mismatches)

	if mismatches > 0 {
		fmt.Printf("\nWARNING: Found %d proposer mismatches!\n", mismatches)
		return nil
	}

	fmt.Println("\nAll proposers match expected! ✓")
	return nil
}
