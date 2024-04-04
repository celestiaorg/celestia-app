package user_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
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
	signer, err := newSingleSignerFromContext(ctx)
	require.NoError(t, err)

	// Pregenerate all the blobs
	numTxs := 10
	blobs := blobfactory.ManyRandBlobs(t, tmrand.NewRand(), blobfactory.Repeat(2048, numTxs)...)

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
		go func(b *tmproto.Blob) {
			defer wg.Done()
			_, err := signer.SubmitPayForBlob(subCtx, []*tmproto.Blob{b}, user.SetGasLimitAndFee(500_000, appconsts.DefaultMinGasPrice))
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

func newSingleSignerFromContext(ctx testnode.Context) (*user.Signer, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	record, err := ctx.Keyring.Key("validator")
	if err != nil {
		return nil, err
	}
	address, err := record.GetAddress()
	if err != nil {
		return nil, err
	}
	return user.SetupSigner(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, address, encCfg)
}
