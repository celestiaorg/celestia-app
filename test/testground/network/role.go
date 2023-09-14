package network

import (
	"context"
	"fmt"
	"time"

	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

const (
	homeDir          = "/.celestia-app"
	TxSimAccountName = "txsim"
)

// Role is the interface between a testground test entrypoint and the actual
// test logic. Testground creates many instances and passes each instance a
// configuration from the plan and manifest toml files. From those
// configurations a Role is created for each node, and the three methods below
// are ran in order.
type Role interface {
	// Plan is the first function called in a test by each node. It is responsible
	// for creating the genesis block and distributing it to all nodes.
	Plan(ctx context.Context, statuses []Status, runenv *runtime.RunEnv, initCtx *run.InitContext) error
	// Execute is the second function called in a test by each node. It is
	// responsible for starting the node and/or running any tests.
	Execute(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error
	// Retro is the last function called in a test by each node. It is
	// responsible for collecting any data from the node and/or running any
	// retrospective tests or benchmarks.
	Retro(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error
}

var _ Role = (*Leader)(nil)

var _ Role = (*Follower)(nil)

// NewRole creates a new role based on the role name.
func NewRole(runenv *runtime.RunEnv, initCtx *run.InitContext) (Role, error) {
	seq := initCtx.GlobalSeq
	switch seq {
	// TODO: throw and error if there is more than a single leader
	case 1:
		runenv.RecordMessage("red leader sitting by")
		return &Leader{}, nil
	default:
		runenv.RecordMessage(fmt.Sprintf("red %d sitting by", seq))
		return &Follower{}, nil
	}
}

// Follower is the role for all nodes in a test except for the leader. It is
// responsible for downloading the genesis block and any other configuration
// data from the leader node.
type Follower struct {
	*ConsensusNode
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

	time.Sleep(time.Second)

	// TODO: download reports
	res, err := f.cctx.Client.NetInfo(ctx)
	if err != nil {
		return err
	}

	realIp, err := initCtx.NetClient.GetDataNetworkIP()
	if err != nil {
		return err
	}

	runenv.RecordMessage(fmt.Sprintf("follower waiting for halt height %d chain id %s real ip %s", f.HaltHeight, f.ChainID, realIp.String()), res)
	_, err = f.cctx.WaitForHeight(int64(f.ConsensusNode.HaltHeight))
	return err
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
	runenv.RecordSuccess()
	return nil
}

// Leader is the role for the leader node in a test. It is responsible for
// creating the genesis block and distributing it to all nodes.
type Leader struct {
	*ConsensusNode
}

// Plan is the method that creates and distributes the genesis, configurations,
// and keys for all of the other nodes in the network.
func (l *Leader) Plan(ctx context.Context, statuses []Status, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	runenv.RecordMessage("leader plan")
	params, err := ParseParams(runenv)
	if err != nil {
		return err
	}

	runenv.RecordMessage("params found: %v", params)

	cfg, err := params.StandardConfig(statuses)
	if err != nil {
		return err
	}

	for _, node := range cfg.Nodes {
		runenv.RecordMessage("node mnemonic: %v", node.Keys.AccountMnemonic == "")
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

	time.Sleep(time.Second)

	// TODO: download reports
	res, err := l.cctx.Client.NetInfo(ctx)
	if err != nil {
		return err
	}

	realIp, err := initCtx.NetClient.GetDataNetworkIP()
	if err != nil {
		return err
	}

	runenv.RecordMessage(fmt.Sprintf("leader waiting for halt height %d chain id %s real ip %s", l.HaltHeight, l.ChainID, realIp.String()), res)
	_, err = l.cctx.WaitForHeight(int64(l.ConsensusNode.HaltHeight))
	return err
}

// Retro collects standard data from the leader node and saves it as a file.
// This data includes the block times, rounds required to reach consensus, and
// the block sizes.
func (l *Leader) Retro(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	defer l.ConsensusNode.Stop()

	// TODO: download reports
	res, err := l.cctx.Client.Status(ctx)
	if err != nil {
		return err
	}
	runenv.RecordMessage("leader retro", res)
	runenv.RecordSuccess()
	return nil
}
