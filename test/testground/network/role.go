package network

import (
	"context"
	"fmt"

	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

const (
	homeDir              = "/.celestia-app"
	TxSimAccountName     = "txsim"
	ValidatorGroupID     = "validators"
	LeaderGroupID        = "leeader"
	FollowerGroupID      = "foollower"
	TxSimGroupID         = "txxsim"
	LeaderGlobalSequence = 1
)

// Role is the interface between a testground test entrypoint and the actual
// test logic. Testground creates many instances and passes each instance a
// configuration from the plan and manifest toml files. From those
// configurations a Role is created for each node, and the three methods below
// are ran in order.
type Role interface {
	// Plan is the first function called in a test by each node. It is
	// responsible for creating the genesis block, configuring nodes, and
	// starting the network.
	Plan(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error
	// Execute is the second function called in a test by each node. It is
	// responsible for running any experiments. This is phase where commands are
	// sent and received.
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
	group := runenv.TestGroupID
	switch group {
	case LeaderGroupID:
		runenv.RecordMessage("leader standing by: group %s", runenv.TestGroupID)
		return &Leader{ConsensusNode: &ConsensusNode{}}, nil
	case FollowerGroupID:
		runenv.RecordMessage(fmt.Sprintf("follower standing by: group %s", runenv.TestGroupID))
		return NewFollower(), nil
	case TxSimGroupID:
		runenv.RecordMessage(fmt.Sprintf("txsim standing by: group %s", runenv.TestGroupID))
		return NewTxSim(), nil
	default:
		return nil, fmt.Errorf("unknown role: %s", group)
	}
}
