package network

import (
	"context"
	"fmt"
	"time"

	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

// Leader is the role for the leader node in a test. It is responsible for
// creating the genesis block and distributing it to all nodes.
type Leader struct {
	*ConsensusNode
}

// Plan is the method that creates and distributes the genesis, configurations,
// and keys for all of the other nodes in the network.
func (l *Leader) Plan(ctx context.Context, statuses []Status, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	params, err := ParseParams(runenv)
	if err != nil {
		return err
	}

	runenv.RecordMessage("params found: %v", params)

	cfg, err := params.StandardConfig(statuses)
	if err != nil {
		return err
	}

	err = PublishConfig(ctx, initCtx, cfg)
	if err != nil {
		return err
	}

	// set the local cosnensus node
	l.ConsensusNode, err = cfg.ConsensusNode(int(initCtx.GlobalSeq))

	return err
}

func (l *Leader) Execute(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	baseDir, err := l.ConsensusNode.Init(homeDir)
	if err != nil {
		return err
	}

	err = l.ConsensusNode.StartNode(ctx, baseDir)
	if err != nil {
		return err
	}

	// issue a command to start txsim
	cmd := NewRunTxSimCommand(
		"txsim",
		time.Minute*10,
		RunTxSimCommandArgs{
			BlobSequences: 10,
			BlobSize:      50_000,
			BlobCount:     2,
		},
	)

	_, err = initCtx.SyncClient.Publish(ctx, CommandTopic, cmd)
	if err != nil {
		return err
	}

	runenv.RecordMessage(fmt.Sprintf("leader waiting for halt height %d", l.HaltHeight))
	_, err = l.cctx.WaitForHeightWithTimeout(int64(l.ConsensusNode.HaltHeight), time.Minute*30)
	if err != nil {
		return err
	}

	_, err = initCtx.SyncClient.Publish(ctx, CommandTopic, EndTestCommand())

	return err
}

// Retro collects standard data from the leader node and saves it as a file.
// This data includes the block times, rounds required to reach consensus, and
// the block sizes.
func (l *Leader) Retro(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	defer l.ConsensusNode.Stop()

	blockRes, err := l.cctx.Client.Block(ctx, nil)
	if err != nil {
		return err
	}

	maxBlockSize := 0
	for i := int64(1); i < blockRes.Block.Height; i++ {
		blockRes, err := l.cctx.Client.Block(ctx, nil)
		if err != nil {
			return err
		}
		size := blockRes.Block.Size()
		if size > maxBlockSize {
			maxBlockSize = size
		}
	}

	runenv.RecordMessage(fmt.Sprintf("leader retro: height %d max block size bytes %d", blockRes.Block.Height, maxBlockSize))

	return nil
}
