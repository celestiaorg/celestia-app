package user_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cometbft/cometbft/config"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/random"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
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
			tmConfig.Consensus.TimeoutCommit = 10 * time.Second
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

// TestTwinsTxClients tests two tx clients using the same account submitting transactions concurrently.
// This test verifies that sequence number management works correctly when multiple clients
// share the same account and that all transactions eventually succeed.
func TestTwinsTxClients(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}

	// Setup network with a clean testnode
	ctx, _, _ := testnode.NewNetwork(t, testnode.DefaultConfig().WithFundedAccounts("alice"))
	_, err := ctx.WaitForHeight(1)
	require.NoError(t, err)

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// Create first tx client
	txClient1, err := user.SetupTxClient(
		ctx.GoContext(),
		ctx.Keyring,
		ctx.GRPCClient,
		encCfg,
		user.InTrustedMode(),
	)
	require.NoError(t, err)

	// Create second tx client using the same account by sharing the same keyring and setting the same default account
	txClient2, err := user.SetupTxClient(
		ctx.GoContext(),
		ctx.Keyring,
		ctx.GRPCClient,
		encCfg,
		user.WithDefaultAccount(txClient1.DefaultAccountName()),
		user.InTrustedMode(),
	)
	require.NoError(t, err)

	// Verify both clients use the same account
	require.Equal(t, txClient1.DefaultAccountName(), txClient2.DefaultAccountName())
	require.Equal(t, txClient1.DefaultAddress(), txClient2.DefaultAddress())

	numTxsPerClient := 20
	totalTxs := numTxsPerClient * 2

	// Prepare blobs for transactions
	blobs1 := blobfactory.ManyRandBlobs(random.New(), blobfactory.Repeat(1024, numTxsPerClient)...)
	blobs2 := blobfactory.ManyRandBlobs(random.New(), blobfactory.Repeat(1024, numTxsPerClient)...)

	var (
		wg        sync.WaitGroup
		errCh     = make(chan error, totalTxs)
		successCh = make(chan string, totalTxs) // Channel to track successful tx hashes
	)

	subCtx, cancel := context.WithTimeout(ctx.GoContext(), 2*time.Minute)
	defer cancel()

	// Function to submit transactions from a client
	submitFromClient := func(client *user.TxClient, clientBlobs []*share.Blob, clientName string) {
		defer wg.Done()
		for i, blob := range clientBlobs {
			wg.Add(1)
			go func(b *share.Blob, txIndex int, name string) {
				defer wg.Done()
				resp, err := client.SubmitPayForBlob(subCtx, []*share.Blob{b}, user.SetGasLimitAndGasPrice(500_000, appconsts.DefaultMinGasPrice))
				if err != nil {
					errCh <- fmt.Errorf("client %s tx %d failed: %w", name, txIndex, err)
				} else {
					successCh <- resp.TxHash
					t.Logf("Client %s tx %d succeeded: %s", name, txIndex, resp.TxHash)
				}
			}(blob, i, clientName)
		}
	}

	// Start both clients submitting transactions concurrently
	wg.Add(2)
	go submitFromClient(txClient1, blobs1, "client1")
	go submitFromClient(txClient2, blobs2, "client2")

	// Wait for all submissions to complete
	wg.Wait()
	close(errCh)
	close(successCh)

	// Check for any errors
	for err := range errCh {
		require.NoError(t, err)
	}

	// Collect successful transaction hashes
	successfulHashes := make([]string, 0, totalTxs)
	for hash := range successCh {
		successfulHashes = append(successfulHashes, hash)
	}

	require.Len(t, successfulHashes, totalTxs, "Expected %d successful transactions, got %d", totalTxs, len(successfulHashes))

	// Verify all transaction hashes are unique (no duplicates)
	hashSet := make(map[string]bool)
	for _, hash := range successfulHashes {
		require.False(t, hashSet[hash], "Duplicate transaction hash detected: %s", hash)
		hashSet[hash] = true
	}

	t.Logf("Successfully submitted %d transactions from 2 twin clients using the same account", totalTxs)
	t.Logf("All transaction hashes are unique, confirming proper sequence number management")
}
