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
	mempools := []string{config.MempoolTypeFlood, config.MempoolTypeFlood} // TODO: add CAT
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
