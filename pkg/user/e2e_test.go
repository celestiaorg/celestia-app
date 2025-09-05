package user_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app/params"
	"github.com/celestiaorg/celestia-app/v6/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/cometbft/cometbft/config"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
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
			numTxs := 20
			blobs := blobfactory.ManyRandBlobs(random.New(), blobfactory.Repeat(2048, numTxs)...)

			sendToSelfMsg := bank.NewMsgSend(txClient.DefaultAddress(), txClient.DefaultAddress(), sdktypes.NewCoins(sdktypes.NewInt64Coin(params.BondDenom, 10)))

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
				go func(b *share.Blob, i int) {
					defer wg.Done()
					var err error
					// send either a send msg of a pay for blob msg
					if i%2 == 0 {
						_, err = txClient.SubmitPayForBlob(subCtx, []*share.Blob{b})
					} else {
						_, err = txClient.SubmitTx(subCtx, []sdktypes.Msg{sendToSelfMsg})
					}
					if err != nil && !errors.Is(err, context.Canceled) {
						// only catch the first error
						select {
						case errCh <- err:
							cancel()
						default:
						}
					}
				}(blobs[i], i)
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
