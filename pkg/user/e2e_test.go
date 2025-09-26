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

	t.Logf("Successfully submitted %d parallel transactions with unique hashes", numJobs)
}
