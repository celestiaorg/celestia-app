package user_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/celestia-app/v6/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cometbft/cometbft/config"
	"github.com/stretchr/testify/require"
)

func TestConcurrentTxSubmission(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Iterate over all mempool versions
	mempools := []string{config.MempoolTypeFlood, config.MempoolTypePriority, config.MempoolTypeCAT}
	for _, mempool := range mempools {
		t.Run(fmt.Sprintf("mempool %s", mempool), func(t *testing.T) {
			// Setup network
			tmConfig := testnode.DefaultTendermintConfig()
			tmConfig.Mempool.Type = mempool
			tmConfig.Consensus.TimeoutCommit = 5 * time.Second
			ctx, _, _ := testnode.NewNetwork(t, testnode.DefaultConfig().WithTendermintConfig(tmConfig))
			_, err := ctx.WaitForHeight(1)
			require.NoError(t, err)

			// Setup signer
			txClient, err := testnode.NewTxClientFromContext(ctx)
			require.NoError(t, err)

			// Pregenerate all the blobs
			numTxs := 100
			blobs := blobfactory.ManyRandBlobs(random.New(), blobfactory.Repeat(2048, numTxs)...)

			// Prepare transactions
			var (
				wg    sync.WaitGroup
				errCh = make(chan error, 1)
			)

<<<<<<< HEAD
			subCtx, cancel := context.WithCancel(ctx.GoContext())
			defer cancel()
			time.AfterFunc(time.Minute, cancel)
			for i := 0; i < numTxs; i++ {
				wg.Add(1)
				go func(b *share.Blob) {
					defer wg.Done()
					_, err := txClient.SubmitPayForBlob(subCtx, []*share.Blob{b}, user.SetGasLimitAndGasPrice(500_000, appconsts.DefaultMinGasPrice))
					if err != nil && !errors.Is(err, context.Canceled) {
						// only catch the first error
						select {
						case errCh <- err:
							cancel()
						default:
						}
					}
				}(blobs[i])
=======
	// Submit jobs in parallel using goroutines
	var wg sync.WaitGroup
	errCh := make(chan error, numJobs)
	for i := range numJobs {
		wg.Add(1)
		go func(idx int) { //nolint:contextcheck
			defer wg.Done()
			resp, err := txClient.SubmitPayForBlobToQueue(ctx.GoContext(), []*share.Blob{blobs[idx]}, user.SetGasLimitAndGasPrice(500_000, appconsts.DefaultMinGasPrice))
			if err != nil {
				errCh <- fmt.Errorf("transaction %d failed: %w", idx, err)
				return
>>>>>>> 8025e18 (refactor: revert changes to SubmitPFB and add new method SubmitPFBInQueue instead (#5935))
			}
			wg.Wait()
			select {
			case err := <-errCh:
				require.NoError(t, err)
			default:
			}
		})
	}
<<<<<<< HEAD
=======

	// Wait for all to complete
	wg.Wait()
	close(errCh)

	// Check for any errors
	for err := range errCh {
		require.NoError(t, err)
	}

	t.Logf("Successfully submitted %d parallel transactions", numJobs)

	// Part 2: Test shutdown behavior and verify pool can be stopped properly
	t.Log("Testing parallel pool shutdown...")

	// Stop the parallel pool
	txClient.StopTxQueueForTest()
	require.False(t, txClient.IsTxQueueStartedForTest())

	// Verify that new submissions fail when tx queue is stopped
	_, err = txClient.SubmitPayForBlobToQueue(ctx.GoContext(), []*share.Blob{blobs[0]})
	require.Error(t, err)
	require.Contains(t, err.Error(), "tx queue not started")

	t.Log("Successfully verified parallel pool shutdown behavior")

	// Part 3: Test restart scenario - create new client and verify existing accounts are reused
	t.Log("Testing restart scenario with existing worker accounts...")

	// Create a new TxClient using the same keyring (simulating restart)
	// Use the same default account as the first client to avoid using a worker account as default
	originalDefaultAccount := txClient.DefaultAccountName()
	txClient2, err := user.SetupTxClient(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, encCfg, user.WithTxWorkers(numWorkers*3), user.WithDefaultAccount(originalDefaultAccount))
	require.NoError(t, err)

	// Tx queue should already be started by SetupTxClient
	require.True(t, txClient2.IsTxQueueStartedForTest())

	// Submit jobs in parallel using goroutines
	var wg2 sync.WaitGroup
	errCh2 := make(chan error, numJobs)
	for i := range numJobs {
		wg2.Add(1)
		go func(idx int) {
			defer wg2.Done()
			resp, err := txClient2.SubmitPayForBlobToQueue(ctx.GoContext(), []*share.Blob{blobs[idx]}, user.SetGasLimitAndGasPrice(500_000, appconsts.DefaultMinGasPrice))
			if err != nil {
				errCh2 <- fmt.Errorf("transaction %d failed: %w", idx, err)
				return
			}
			if resp == nil || resp.TxHash == "" {
				errCh2 <- fmt.Errorf("transaction %d returned nil or empty response", idx)
			}
		}(i)
	}

	// Wait for all to complete
	wg2.Wait()
	close(errCh2)

	// Check for any errors
	for err := range errCh2 {
		require.NoError(t, err)
	}

	// Check that worker accounts exist in keyring before initialization
	for i := 1; i < numWorkers*3; i++ {
		accountName := fmt.Sprintf("parallel-worker-%d", i)
		_, err := ctx.Keyring.Key(accountName)
		require.NoError(t, err, "worker account %s should exist in keyring", accountName)
	}

	t.Log("Successfully verified restart scenario - existing worker accounts were reused and no new accounts were created")
>>>>>>> 8025e18 (refactor: revert changes to SubmitPFB and add new method SubmitPFBInQueue instead (#5935))
}
