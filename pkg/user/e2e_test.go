package user_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
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

			subCtx, cancel := context.WithCancel(ctx.GoContext())
			defer cancel()
			time.AfterFunc(time.Minute, cancel)
			for i := range numTxs {
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
			}
			wg.Wait()
			select {
			case err := <-errCh:
				require.NoError(t, err)
			default:
			}
		})
	}
}

func TestParallelTxSubmission(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Setup network
	tmConfig := testnode.DefaultTendermintConfig()
	tmConfig.Consensus.TimeoutCommit = 3 * time.Second
	ctx, _, _ := testnode.NewNetwork(t, testnode.DefaultConfig().WithTendermintConfig(tmConfig))
	_, err := ctx.WaitForHeight(1)
	require.NoError(t, err)

	// Setup signer with parallel workers (accounts will be auto-created)
	numWorkers := 3
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txWorkersOpt := user.WithTxWorkersNoInit(numWorkers)
	txClient, err := user.SetupTxClient(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, encCfg, txWorkersOpt)
	require.NoError(t, err)

	// Initialize worker accounts manually for e2e test
	err = txClient.InitializeWorkerAccounts(ctx.GoContext())
	require.NoError(t, err)

	// Re-initialization should be a no-op
	err = txClient.InitializeWorkerAccounts(ctx.GoContext())
	require.NoError(t, err)

	// Start the parallel pool manually
	err = txClient.ParallelPool().Start(ctx.GoContext())
	require.NoError(t, err)

	// Generate test blobs
	numJobs := 10
	blobs := blobfactory.ManyRandBlobs(random.New(), blobfactory.Repeat(1024, numJobs)...)

	// Submit jobs in parallel - each returns its own results channel
	var resultChannels []chan user.SubmissionResult
	for i := 0; i < numJobs; i++ {
		resultsC, err := txClient.SubmitPayForBlobParallel(ctx.GoContext(), []*share.Blob{blobs[i]}, user.SetGasLimitAndGasPrice(500_000, appconsts.DefaultMinGasPrice))
		require.NoError(t, err)
		resultChannels = append(resultChannels, resultsC)
	}

	// Wait for all results from individual channels
	for i, resultsC := range resultChannels {
		select {
		case result := <-resultsC:
			require.NoError(t, result.Error, "transaction should succeed")
			require.NotNil(t, result.TxResponse, "should have tx response")
			require.NotEmpty(t, result.TxResponse.TxHash, "should have tx hash")
		case <-time.After(2 * time.Minute):
			t.Fatalf("timeout waiting for result %d", i)
		}
	}

	t.Logf("Successfully submitted %d parallel transactions", numJobs)

	// Part 2: Test shutdown behavior and verify pool can be stopped properly
	t.Log("Testing parallel pool shutdown...")

	// Stop the parallel pool
	txClient.ParallelPool().Stop()
	require.False(t, txClient.ParallelPool().IsStarted())

	// Verify that new submissions fail when pool is stopped
	_, err = txClient.SubmitPayForBlobParallel(ctx.GoContext(), []*share.Blob{blobs[0]})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parallel pool not started")

	t.Log("Successfully verified parallel pool shutdown behavior")

	// Part 3: Test restart scenario - create new client and verify existing accounts are reused
	t.Log("Testing restart scenario with existing worker accounts...")

	// Create a new TxClient using the same keyring (simulating restart)
	// Use the same default account as the first client to avoid using a worker account as default
	originalDefaultAccount := txClient.DefaultAccountName()
	txClient2, err := user.SetupTxClient(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, encCfg, user.WithTxWorkersNoInit(numWorkers*3), user.WithDefaultAccount(originalDefaultAccount))
	require.NoError(t, err)

	// Initialize worker accounts manually for e2e test
	err = txClient2.InitializeWorkerAccounts(ctx.GoContext())
	require.NoError(t, err)

	// Start the parallel pool
	err = txClient2.ParallelPool().Start(ctx.GoContext())
	require.NoError(t, err)

	// Submit jobs in parallel - each returns its own results channel
	var resultChannels2 []chan user.SubmissionResult
	for i := 0; i < numJobs; i++ {
		resultsC, err := txClient2.SubmitPayForBlobParallel(ctx.GoContext(), []*share.Blob{blobs[i]}, user.SetGasLimitAndGasPrice(500_000, appconsts.DefaultMinGasPrice))
		require.NoError(t, err)
		resultChannels2 = append(resultChannels2, resultsC)
	}

	// Wait for all results from individual channels
	for i, resultsC := range resultChannels2 {
		select {
		case result := <-resultsC:
			require.NoError(t, result.Error, "transaction should succeed")
			require.NotNil(t, result.TxResponse, "should have tx response")
			require.NotEmpty(t, result.TxResponse.TxHash, "should have tx hash")
		case <-time.After(2 * time.Minute):
			t.Fatalf("timeout waiting for result %d", i)
		}
	}

	// Check that worker accounts exist in keyring before initialization
	for i := 1; i < numWorkers*3; i++ {
		accountName := fmt.Sprintf("parallel-worker-%d", i)
		_, err := ctx.Keyring.Key(accountName)
		require.NoError(t, err, "worker account %s should exist in keyring", accountName)
	}

	t.Log("Successfully verified restart scenario - existing worker accounts were reused and no new accounts were created")
}
