package network

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

var _ Role = &TxSim{}

type TxSim struct {
	params *Params
	Name   string
	ecfg   encoding.Config
	kr     keyring.Keyring
	op     *Operator
	pp     []PeerPacket
	gs     int
}

func NewTxSim() *TxSim {
	t := &TxSim{}
	op := NewOperator()
	op.RegisterCommand(
		RunTxSimCommandID,
		func(ctx context.Context, runenv *runtime.RunEnv, _ *run.InitContext, args json.RawMessage) error {
			var a RunTxSimCommandArgs
			err := json.Unmarshal(args, &a)
			if err != nil {
				return err
			}
			runenv.RecordMessage("txsim: running txsim")
			return t.RunTxSim(ctx, a)
		},
	)

	t.op = op
	return t
}

// Plan is the first function called in a test by each node. It is
// responsible for creating the genesis block, configuring nodes, and
// starting the network.
func (t *TxSim) Plan(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	runenv.RecordMessage("Bootstrapping txsim")
	pp, err := t.Bootstrap(ctx, runenv, initCtx)
	if err != nil {
		return err
	}
	t.pp = pp
	t.gs = int(initCtx.GlobalSeq)

	return nil
}

// Execute is the second function called in a test by each node. It is
// responsible for running any experiments. This is phase where commands are
// sent and received.
func (t *TxSim) Execute(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	runenv.RecordMessage("txsim Listening for commands")
	return t.ListenForCommands(ctx, runenv, initCtx)
}

// Retro is the last function called in a test by each node. It is
// responsible for collecting any data from the node and/or running any
// retrospective tests or benchmarks.
func (t *TxSim) Retro(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	return nil
}

func (t *TxSim) Bootstrap(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) ([]PeerPacket, error) {
	t.ecfg = encoding.MakeConfig(app.ModuleBasics)

	params, err := ParseParams(t.ecfg, runenv)
	if err != nil {
		return nil, err
	}
	t.params = params

	nodeID := NodeID(initCtx.GlobalSeq)
	t.Name = nodeID

	kr, addrs := testnode.NewKeyring(nodeID, TxSimAccountName)
	t.kr = kr

	pubKs, err := getPublicKeys(t.kr, nodeID, TxSimAccountName)
	if err != nil {
		return nil, err
	}

	pp := PeerPacket{
		PeerID:          "txsim",
		GroupID:         runenv.TestGroupID,
		GlobalSequence:  initCtx.GlobalSeq,
		GenesisAccounts: addrsToStrings(addrs...),
		GenesisPubKeys:  pubKs,
	}

	_, err = initCtx.SyncClient.Publish(ctx, PeerPacketTopic, pp)
	if err != nil {
		return nil, err
	}

	packets, err := DownloadSync(ctx, initCtx, PeerPacketTopic, PeerPacket{}, runenv.TestInstanceCount)
	if err != nil {
		return nil, err
	}

	return packets, nil
}

func (t *TxSim) ListenForCommands(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	cmds := make(chan Command)
	defer close(cmds)
	_, err := initCtx.SyncClient.Subscribe(ctx, CommandTopic, cmds)
	if err != nil {
		return err
	}
	// run will block until the context is canceled or the leader sends a
	// command to stop.
	return t.op.Run(ctx, runenv, initCtx, cmds)
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
	BlobSequences int            `json:"blob_sequences"`
	BlobSize      int            `json:"blob_size"`
	BlobCount     int            `json:"blob_count"`
	IPs           map[int]string `json:"ips"`
}

func (c *RunTxSimCommandArgs) Sequences() []txsim.Sequence {
	return txsim.NewBlobSequence(
		txsim.NewRange(c.BlobSize, c.BlobSize),
		txsim.NewRange(c.BlobCount, c.BlobCount)).
		Clone(c.BlobSequences)
}

// RunTxSim runs the txsim tool on the follower node.
func (t *TxSim) RunTxSim(ctx context.Context, c RunTxSimCommandArgs) error {
	time.Sleep(10 * time.Second) // magic wait time. Testground takes a while.
	grpcEndpoint, has := c.IPs[t.gs]
	if !has {
		return fmt.Errorf("no grpc endpoint found for txsim global sequence %d", t.gs)
	}
	opts := txsim.DefaultOptions().UseFeeGrant().SuppressLogs().SpecifyMasterAccount("txsim")
	return txsim.Run(ctx, grpcEndpoint+":9090", t.kr, t.ecfg, opts, c.Sequences()...)
}
