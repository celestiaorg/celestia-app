package network

import (
	"context"
	"encoding/json"
	"errors"
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
		func(ctx context.Context, runenv *runtime.RunEnv, _ *run.InitContext, args json.RawMessage) error {
			var a RunTxSimCommandArgs
			err := json.Unmarshal(args, &a)
			if err != nil {
				return err
			}
			runenv.RecordMessage("running txsim")
			return f.RunTxSim(ctx, a)
		},
	)

	op.RegisterCommand(RunSubmitRandomPFBs, f.SubmitRandomPFBsHandler)

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
	RunTxSimCommandID   = "run_txsim"
	RunSubmitRandomPFBs = "submit-random-pfbs"
)

func NewRunTxSimCommand(id string, timeout time.Duration, args RunTxSimCommandArgs) Command {
	bz, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	cmd := Command{
		ID:          id,
		Name:        RunTxSimCommandID,
		Args:        bz,
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
	grpcEndpoint := "127.0.0.1:9090"
	opts := txsim.DefaultOptions().UseFeeGrant().SuppressLogs()
	return txsim.Run(ctx, grpcEndpoint, f.kr, f.ecfg, opts, c.Sequences()...)
}

func NewSubmitRandomPFBsCommand(id string, timeout time.Duration, sizes ...int) Command {
	bz, err := json.Marshal(sizes)
	if err != nil {
		panic(err)
	}

	return Command{
		ID:          id,
		Name:        RunSubmitRandomPFBs,
		Args:        bz,
		Timeout:     timeout,
		TargetGroup: "all",
	}
}

func (c *ConsensusNode) SubmitRandomPFBsHandler(
	ctx context.Context,
	runenv *runtime.RunEnv,
	initCtx *run.InitContext,
	args json.RawMessage,
) error {
	var sizes []int
	err := json.Unmarshal(args, &sizes)
	if err != nil {
		return err
	}
	runenv.RecordMessage("called handler")
	for {
		select {
		case <-ctx.Done():
			runenv.RecordMessage("done with handler")
			return nil
		default:
			runenv.RecordMessage("calling suvbmit")
			resp, err := c.SubmitRandomPFB(ctx, runenv, sizes...)
			if err != nil {
				return err
			}
			runenv.RecordMessage("received a response")
			if resp == nil {
				return errors.New("nil response and nil error submitting PFB")
			}
			runenv.RecordMessage(fmt.Sprintf("follower submitted PFB code: %d %s", resp.Code, resp.Codespace))
		}
	}
}
