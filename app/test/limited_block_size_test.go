package app_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLimitedBlockSize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Limited Block Size integration test in short mode.")
	}

	desiredSquareSize := uint64(64)

	cparams := testnode.DefaultParams()
	// limit the max block size to 64 x 64 via the consensus parameter
	cparams.Block.MaxBytes = square.EstimateMaxBlockBytes(desiredSquareSize)

	kr, rpcAddr, grpcAddr := txsim.Setup(t, cparams)

	// using lots of individual small blobs will result in a large amount of
	// overhead added to the square
	seqs := txsim.NewBlobSequence(
		txsim.NewRange(1, 10000),
		txsim.NewRange(1, 50),
	).Clone(25)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	_ = txsim.Run(
		ctx,
		[]string{rpcAddr},
		[]string{grpcAddr},
		kr,
		rand.Int63(),
		time.Second,
		seqs...,
	)

	// check the block sizes
	blocks, err := testnode.ReadBlockchain(context.Background(), rpcAddr)
	require.NoError(t, err)

	for _, block := range blocks {
		fmt.Println(block.Data.SquareSize)
		assert.LessOrEqual(t, block.Data.SquareSize, desiredSquareSize)
	}

}
