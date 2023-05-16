package app_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestrictedBlockSize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping restricted block size integration test in short mode.")
	}

	type test struct {
		name             string
		maxBytes         int64
		govMaxSquareSize uint64
	}
	tests := []test{
		{
			"default (should be 64 as of mainnet)",
			appconsts.DefaultMaxBytes,
			appconsts.DefaultGovMaxSquareSize,
		},
		// the testnode cannot consistently be started and stopped in the same
		// test, so we only run this test once. See
		// TestPrepareProposalConsistency for a simulated version of the below
		// tests
		//
		// {
		//  "max",
		//  appconsts.MaxShareCount * appconsts.ContinuationSparseShareContentSize,
		//  appconsts.MaxSquareSize,
		// }, {
		//  "over max (square size 256)",
		//  256 * 256 * appconsts.ContinuationSparseShareContentSize,
		//  appconsts.MaxSquareSize,
		// }, {
		//  "square size of 32",
		//  32 * 32 * appconsts.ContinuationSparseShareContentSize,
		//  32,
		// },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cparams := testnode.DefaultParams()
			cparams.Block.MaxBytes = tt.maxBytes

			cctx, rpcAddr, grpcAddr := testnode.NewNetwork(
				t,
				cparams,
				testnode.DefaultTendermintConfig(),
				testnode.DefaultAppConfig(),
				[]string{},
			)

			// using lots of individual small blobs will result in a large amount of
			// overhead added to the square, which helps ensure we are hitting large squares
			seqs := txsim.NewBlobSequence(
				txsim.NewRange(1, 10000),
				txsim.NewRange(20, 100),
			).Clone(20)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
			defer cancel()

			_ = txsim.Run(
				ctx,
				[]string{rpcAddr},
				[]string{grpcAddr},
				cctx.Keyring,
				rand.Int63(),
				time.Second,
				seqs...,
			)

			// check the block sizes
			blocks, err := testnode.ReadBlockchain(context.Background(), rpcAddr)
			require.NoError(t, err)

			atMax := 0
			for _, block := range blocks {
				assert.LessOrEqual(t, block.Data.SquareSize, tt.govMaxSquareSize)
				if block.Data.SquareSize == tt.govMaxSquareSize {
					atMax++
				}
			}
			// check that at least one block was at or above the goal size
			assert.GreaterOrEqual(t, atMax, 1)
		})
	}
}
