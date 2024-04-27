package network

import (
	"context"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/test/util/sdkutil"
	"github.com/cosmos/cosmos-sdk/codec"
	coretypes "github.com/tendermint/tendermint/types"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

const (
	UnboundedBlockSize = "unbounded"
	ConsistentFill     = "consistent-fill"
)

func fillBlocks(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext, timeout time.Duration) (RunTxSimCommandArgs, error) {
	seqs := runenv.IntParam(BlobSequencesParam)
	size := runenv.IntParam(BlobSizesParam)
	count := runenv.IntParam(BlobsPerSeqParam)

	args := RunTxSimCommandArgs{
		BlobSequences: seqs,
		BlobSize:      size,
		BlobCount:     count,
	}

	cmd := NewRunTxSimCommand("txsim-0", timeout, args)

	runenv.RecordMessage("leader: sending txsim command")

	_, err := initCtx.SyncClient.Publish(ctx, CommandTopic, cmd)
	return args, err
}

// unboundedBlockSize increases the block size until either the test times out
// (1h by default) or the ability to reach consensus is lost.
func (l *Leader) unboundedBlockSize(
	ctx context.Context,
	runenv *runtime.RunEnv,
	initCtx *run.InitContext,
	cdc codec.Codec,
	heightStepSize int64,
) error {
	args, err := fillBlocks(ctx, runenv, initCtx, time.Minute*59)
	if err != nil {
		return err
	}

	go l.RunTxSim(ctx, args)

	query := "tm.event = 'NewBlockHeader'"
	events, err := l.cctx.Client.Subscribe(ctx, "leader", query, 10)
	if err != nil {
		return err
	}

	go func() {
		// blockSize is the starting block size limit in bytes. This is
		// incremented by blockIncrement each loop.
		blockSize := 1800000
		// blockIncrement is the amount the block size limit is increased in
		// bytes by each loop.
		blockIncrement := 5000000
		proposalCount := uint64(1)
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-events:
				newHeader, ok := ev.Data.(coretypes.EventDataNewBlockHeader)
				if !ok {
					panic(fmt.Sprintf("unexpected event type: %T", ev.Data))
				}

				if newHeader.Header.Height%heightStepSize != 0 {
					continue
				}

				err = l.changeParams(ctx, runenv, proposalCount, sdkutil.MaxBlockBytesParamChange(cdc, blockSize))
				if err != nil {
					runenv.RecordMessage("leader: failure to increase the blocksize %d, %v", blockSize, err)
					runenv.RecordFailure(err)
					return
				}
				runenv.RecordMessage("leader: changed max block size to %d", blockSize)
				blockSize += blockIncrement
				blockIncrement += (blockSize * 2)
				proposalCount++
			}
		}
	}()

	return nil
}
