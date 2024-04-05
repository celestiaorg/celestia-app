package user_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/celestiaorg/go-square/blob"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestConcurrentTxSubmission(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Setup network
	tmConfig := testnode.DefaultTendermintConfig()
	tmConfig.Consensus.TimeoutCommit = 10 * time.Second
	ctx, _, _ := testnode.NewNetwork(t, testnode.DefaultConfig().WithTendermintConfig(tmConfig))
	_, err := ctx.WaitForHeight(1)
	require.NoError(t, err)

	// Setup signer
	signer, err := testnode.NewSingleSignerFromContext(ctx)
	require.NoError(t, err)

	// Pregenerate all the blobs
	numTxs := 10
	blobs := blobfactory.ManyRandBlobs(tmrand.NewRand(), blobfactory.Repeat(2048, numTxs)...)

	// Prepare transactions
	var (
		wg    sync.WaitGroup
		errCh = make(chan error)
	)

	subCtx, cancel := context.WithCancel(ctx.GoContext())
	defer cancel()
	time.AfterFunc(time.Minute, cancel)
	for i := 0; i < numTxs; i++ {
		wg.Add(1)
		go func(b *blob.Blob) {
			defer wg.Done()
			_, err := signer.SubmitPayForBlob(subCtx, []*blob.Blob{b}, user.SetGasLimitAndFee(500_000, appconsts.DefaultMinGasPrice))
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
}

func TestSignerTwins(t *testing.T) {
	// Ref: https://github.com/celestiaorg/celestia-app/issues/3256
	t.Skip()

	// Setup network
	tmConfig := testnode.DefaultTendermintConfig()
	tmConfig.Consensus.TimeoutCommit = 10 * time.Second
	ctx, _, _ := testnode.NewNetwork(t, testnode.DefaultConfig().WithTendermintConfig(tmConfig))
	_, err := ctx.WaitForHeight(1)
	require.NoError(t, err)

	signer1, err := testnode.NewSingleSignerFromContext(ctx)
	require.NoError(t, err)
	signer2, err := testnode.NewSingleSignerFromContext(ctx)
	require.NoError(t, err)

	blobs := blobfactory.ManyRandBlobs(tmrand.NewRand(), blobfactory.Repeat(2048, 8)...)

	_, err = signer1.SubmitPayForBlob(ctx.GoContext(), blobs[:1], user.SetGasLimitAndFee(500_000, appconsts.DefaultMinGasPrice))
	require.NoError(t, err)

	_, err = signer2.SubmitPayForBlob(ctx.GoContext(), blobs[1:3], user.SetGasLimitAndFee(500_000, appconsts.DefaultMinGasPrice))
	require.NoError(t, err)

	signer1.ForceSetSequence(4)
	_, err = signer1.SubmitPayForBlob(ctx.GoContext(), blobs[3:5], user.SetGasLimitAndFee(500_000, appconsts.DefaultMinGasPrice))
	require.NoError(t, err)
}
