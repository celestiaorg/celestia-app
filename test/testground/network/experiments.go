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

func init() {
	// coretypes.SetBlockPartSizeBytes(3220000)
	// consensus.SetMaxMsgSize(5000000)
	// consensus.SetBlockPartPriority(50)
	// consensus.SetRecvBufferCapacity(100 * 50 * 4096)
	// consensus.SetSendQueueCapacity(10000)
}

func fillBlocks(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext, timeout time.Duration) error {
	seqs := runenv.IntParam(BlobSequencesParam)
	size := runenv.IntParam(BlobSizesParam)
	count := runenv.IntParam(BlobsPerSeqParam)

	cmd := NewRunTxSimCommand("txsim-0", timeout, RunTxSimCommandArgs{
		BlobSequences: seqs,
		BlobSize:      size,
		BlobCount:     count,
	})

	runenv.RecordMessage("leader: sending txsim command")

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
		blockIncrement := 5000000
		sleep := time.Second * 60
		sleepIncrement := time.Second * 30
		proposalCount := uint64(1)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				err = l.changeParams(ctx, runenv, proposalCount, sdkutil.MaxBlockBytesParamChange(cdc, blockSize))
				if err != nil {
					runenv.RecordMessage("leader: failure to increase the blocksize %d, %v", blockSize, err)
					runenv.RecordFailure(err)
					return
				}
				runenv.RecordMessage("leader: changed max block size to %d", blockSize)
				time.Sleep(sleep)
				sleep += sleepIncrement
				sleepIncrement += time.Second * 15
				blockSize += blockIncrement
				blockIncrement += 5000000
				proposalCount++
			}
		}
	}()

	return nil
}
