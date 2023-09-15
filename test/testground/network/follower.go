package network

import (
	"context"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

// Follower is the role for all nodes in a test except for the leader. It is
// responsible for downloading the genesis block and any other configuration
// data from the leader node.
type Follower struct {
	*ConsensusNode
	op *Operator
}

// NewFollower creates a new follower role.
func NewFollower() *Follower {
	f := &Follower{}
	op := NewOperator()
	op.RegisterCommand(
		RunTxSimCommandID,
		func(ctx context.Context, runenv *runtime.RunEnv, _ *run.InitContext, args interface{}) error {
			runenv.RecordMessage("running txsim")
			return f.RunTxSim(ctx, args.(RunTxSimCommandArgs))
		})
	f.op = op
	return f
}

// Plan is the method that downloads the genesis, configurations, and keys for
// all of the other nodes in the network.
func (f *Follower) Plan(ctx context.Context, _ []Status, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	cfg, err := DownloadNetworkConfig(ctx, initCtx)
	if err != nil {
		return err
	}

	f.ConsensusNode, err = cfg.ConsensusNode(int(initCtx.GlobalSeq))
	return err
}

func (f *Follower) Execute(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	baseDir, err := f.ConsensusNode.Init(homeDir)
	if err != nil {
		return err
	}
	err = f.ConsensusNode.StartNode(ctx, baseDir)
	if err != nil {
		return err
	}

	runenv.RecordMessage(fmt.Sprintf("follower %d waiting for commands", f.Status.GlobalSequence))
	return f.ListenForCommands(ctx, runenv, initCtx)
}

// Retro collects standard data from the follower node and saves it as a file.
// This data includes the block times, rounds required to reach consensus, and
// the block sizes.
func (f *Follower) Retro(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	defer f.ConsensusNode.Stop()

	// TODO: publish report
	res, err := f.cctx.Client.Status(ctx)
	if err != nil {
		return err
	}
	runenv.RecordMessage("follower retro", res)
	return nil
}

func (f *Follower) ListenForCommands(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	cmds := make(chan Command)
	defer close(cmds)
	_, err := initCtx.SyncClient.Subscribe(ctx, CommandTopic, cmds)
	if err != nil {
		return err
	}
	// run will block until the context is canceled or the leader sends a
	// command to stop.
	return f.op.Run(ctx, runenv, initCtx, cmds)
}

const (
	RunTxSimCommandID = "run_txsim"
)

func NewRunTxSimCommand(id string, timeout time.Duration, args RunTxSimCommandArgs) Command {
	cmd := Command{
		ID:          id,
		Name:        RunTxSimCommandID,
		Args:        args,
		Timeout:     timeout,
		TargetGroup: "all",
	}
	return cmd
}

type RunTxSimCommandArgs struct {
	// BlobSequences is the number of blob sequences to run
	BlobSequences int `json:"blob_sequences"`
	BlobSize      int `json:"blob_size"`
	BlobCount     int `json:"blob_count"`
}

func (c *RunTxSimCommandArgs) Sequences() []txsim.Sequence {
	return txsim.NewBlobSequence(
		txsim.NewRange(c.BlobSize, c.BlobSize),
		txsim.NewRange(c.BlobCount, c.BlobCount)).
		Clone(c.BlobSequences)
}

// RunTxSim runs the txsim tool on the follower node.
func (f *Follower) RunTxSim(ctx context.Context, c RunTxSimCommandArgs) error {
	grpcEndpoint := "localhost:9090"
	opts := txsim.DefaultOptions().UseFeeGrant().SuppressLogs()
	return txsim.Run(ctx, grpcEndpoint, f.kr, f.ecfg, opts, c.Sequences()...)
}
