package network

import (
	"context"
	"time"

	"github.com/celestiaorg/celestia-app/test/util/sdkutil"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

const (
	UnboundedBlockSize = "unbounded"
	ConsistentFill     = "consistent-fill"
)

func fillBlocks(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext, timeout time.Duration) error {
	seqs := runenv.IntParam(BlobSequencesParam)
	size := runenv.IntParam(BlobSizesParam)
	count := runenv.IntParam(BlobsPerSeqParam)

	cmd := NewRunTxSimCommand("txsim-0", timeout, RunTxSimCommandArgs{
		BlobSequences: seqs,
		BlobSize:      size,
		BlobCount:     count,
	})

	_, err := initCtx.SyncClient.Publish(ctx, CommandTopic, cmd)
	return err
}

func (l *Leader) unboundedBlockSize(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext, cdc codec.Codec) error {
	err := fillBlocks(ctx, runenv, initCtx, time.Minute*50)
	if err != nil {
		return err
	}

	go func() {
		blockSize := 2000000
		for {
			select {
			case <-ctx.Done():
				return
			default:
				err = l.changeParams(ctx, runenv, sdkutil.MaxBlockBytesParamChange(cdc, blockSize))
				if err != nil {
					runenv.RecordFailure(err)
					return
				}
				time.Sleep(time.Minute * 4)
				blockSize += 10000000
			}
		}
	}()

	return nil
}
