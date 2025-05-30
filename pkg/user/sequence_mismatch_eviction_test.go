package user_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/random"
)

// TestSequenceMismatchEviction tests that the TxClient correctly handles evictions without
// causing sequence number desynchronization. This test verifies the fix for issue #4784.
func TestSequenceMismatchEviction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping sequence mismatch eviction test in short mode")
	}

	_, txClient, ctx := setupTxClient(t, 1*time.Nanosecond)

	t.Run("verify eviction handling does not result in a sequence mismatch", func(t *testing.T) {
		testAccount := txClient.DefaultAccountName()

		blobSize := 100000
		blob := blobfactory.ManyRandBlobs(random.New(), blobSize)[0]

		evictionDetected := false

		for i := 0; i < 10; i++ {
			currentSequence := txClient.Account(testAccount).Sequence()

			t.Logf("Submitting blob tx attempt %d with sequence %d", i, currentSequence)

			response, err := txClient.SubmitPayForBlob(ctx.GoContext(), []*share.Blob{blob},
				user.SetFee(50000))

			if err != nil {
				if strings.Contains(err.Error(), "tx was evicted from the mempool") {
					sequenceAfterEviction := txClient.Account(testAccount).Sequence()

					t.Logf("Eviction detected! Sequence after eviction: %d (was %d)", sequenceAfterEviction, currentSequence)
					evictionDetected = true

					// Test that the next transaction does NOT have a sequence mismatch
					t.Run("verify no sequence mismatch after eviction", func(t *testing.T) {
						verifyNoSequenceMismatch(t, ctx.GoContext(), txClient, testAccount)
					})

					break
				} else {
					t.Logf("Transaction failed with error: %v", err)
				}
			} else if response != nil {
				t.Logf("Transaction %s confirmed in block %d", response.TxHash, response.Height)
			}
		}

		if !evictionDetected {
			t.Skip("No eviction detected in this test run - test inconclusive")
		}
	})
}

func verifyNoSequenceMismatch(t *testing.T, ctx context.Context, txClient *user.TxClient, testAccount string) {
	t.Helper()

	account := txClient.Account(testAccount)
	currentSequence := account.Sequence()

	t.Logf("Verifying no sequence mismatch with current sequence %d", currentSequence)

	testBlob := blobfactory.ManyRandBlobs(random.New(), 1000)[0]
	_, err := txClient.SubmitPayForBlob(ctx, []*share.Blob{testBlob})

	if err != nil {
		errorMsg := err.Error()
		t.Logf("Next transaction failed with: %v", err)

		if isSequenceMismatchError(errorMsg) {
			t.Fatalf("SEQUENCE MISMATCH DETECTED: %s - This indicates the fix is not working!", errorMsg)
		} else {
			t.Logf("Transaction failed with non-sequence error (this is expected): %s", errorMsg)
		}
	} else {
		t.Log("Next transaction succeeded - fix is working correctly!")
	}
}

// isSequenceMismatchError checks if an error message indicates a sequence mismatch
func isSequenceMismatchError(errMsg string) bool {
	sequenceMismatchIndicators := []string{
		"account sequence mismatch",
		"incorrect account sequence",
		"expected",
		"sequence",
	}

	errLower := strings.ToLower(errMsg)
	matchCount := 0

	for _, indicator := range sequenceMismatchIndicators {
		if strings.Contains(errLower, indicator) {
			matchCount++
		}
	}

	return matchCount >= 2
}
