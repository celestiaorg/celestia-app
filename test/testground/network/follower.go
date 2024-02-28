package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

// Follower is the role for all nodes in a test except for the leader. It is
// responsible for downloading the genesis block and any other configuration
// data from the leader node.
type Follower struct {
	*ConsensusNode
	op     *Operator
	signer *user.Signer
}

// NewFollower creates a new follower role.
func NewFollower() *Follower {
	f := &Follower{&ConsensusNode{}, nil, nil}
	// all of the commands that the follower can receive have to be registered
	// at some point. This is currently done here.
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
	op.RegisterCommand(
		SubmitTxCommandID,
		func(ctx context.Context, runenv *runtime.RunEnv, _ *run.InitContext, args json.RawMessage) error {
			var a SubmitTxCommandArgs
			err := json.Unmarshal(args, &a)
			if err != nil {
				return err
			}
			runenv.RecordMessage("submitting tx")
			err = f.SubmitTx(ctx, a)
			return err
		},
	)

	f.op = op
	return f
}

// Plan is the method that downloads the genesis, configurations, and keys for
// all of the other nodes in the network.
func (f *Follower) Plan(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	runenv.RecordMessage("follower bootstrapping")
	packets, err := f.Bootstrap(ctx, runenv, initCtx)
	if err != nil {
		return err
	}

	runenv.RecordMessage("follower Downloading Genesis")

	genBz, err := DownloadGenesis(ctx, initCtx)
	if err != nil {
		return err
	}

	runenv.RecordMessage("follower downloading node configs")

	nodes, err := DownloadNodeConfigs(ctx, runenv, initCtx)
	if err != nil {
		return err
	}

	node, has := searchNodes(nodes, initCtx.GlobalSeq)
	if !has {
		return errors.New("node not found")
	}

	err = f.Init(homeDir, genBz, node)
	if err != nil {
		return err
	}

	err = addPeersToAddressBook(f.CmtConfig.P2P.AddrBookFile(), packets)
	if err != nil {
		return err
	}

	err = f.ConsensusNode.StartNode(ctx, f.baseDir)
	if err != nil {
		return err
	}

	if f.CmtConfig.Instrumentation.PyroscopeTrace {
		runenv.RecordMessage("pyroscope: follower starting pyroscope")
	}

	runenv.RecordMessage("follower waiting for start height")

	_, err = f.cctx.WaitForHeightWithTimeout(int64(3), time.Minute*7)
	if err != nil {
		return err
	}

	// if f.CmtConfig.Instrumentation.Prometheus {
	// 	runenv.RecordMessage("profiling: follower starting prometheus")
	// 	go ScrapeMetrics(ctx, "http://51.159.176.205:8080/store_packets", f.cctx.ChainID, initCtx.GlobalSeq, runenv)
	// }

	addr := testfactory.GetAddress(f.cctx.Keyring, f.Name)

	signer, err := user.SetupSigner(ctx, f.cctx.Keyring, f.cctx.GRPCClient, addr, f.ecfg)
	if err != nil {
		runenv.RecordMessage(fmt.Sprintf("follower: failed to setup signer %+v", err))
		return err
	}
	f.signer = signer

	return err
}

func (f *Follower) Execute(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	runenv.RecordMessage("follower waiting for commands")
	return f.ListenForCommands(ctx, runenv, initCtx)
}

// Retro collects standard data from the follower node and saves it as a file.
// This data includes the block times, rounds required to reach consensus, and
// the block sizes.
func (f *Follower) Retro(ctx context.Context, runenv *runtime.RunEnv, _ *run.InitContext) error {
	//nolint:errcheck
	defer f.ConsensusNode.Stop()

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

const (
	SubmitTxCommandID = "submit_tx"
)

type SubmitTxCommandArgs struct {
	Tx []byte `json:"tx"`
}

func NewSubmitTxCommand(id string, timeout time.Duration, args SubmitTxCommandArgs) Command {
	bz, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	cmd := Command{
		ID:          id,
		Name:        SubmitTxCommandID,
		Args:        bz,
		Timeout:     timeout,
		TargetGroup: "all",
	}
	return cmd
}

// SubmitTx submits a transaction to the follower node.
func (f *Follower) SubmitTx(ctx context.Context, c SubmitTxCommandArgs) error {
	resp, err := f.signer.BroadcastTx(ctx, c.Tx)
	if err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("tx failed with code %d: %s", resp.Code, resp.RawLog)
	}
	resp, err = f.signer.ConfirmTx(ctx, resp.TxHash)
	if resp.Code != 0 {
		return fmt.Errorf("tx failed with code %d: %s", resp.Code, resp.RawLog)
	}
	return err
}
