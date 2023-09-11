package network

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/test/genesis"
	cmtjson "github.com/tendermint/tendermint/libs/json"
	"github.com/tendermint/tendermint/p2p"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

const (
	FinishedState    = "finished"
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
	Plan(runenv *runtime.RunEnv, initCtx *run.InitContext) error
	// Execute is the second function called in a test by each node. It is
	// responsible for starting the node and/or running any tests.
	Execute(runenv *runtime.RunEnv, initCtx *run.InitContext) error
	// Retro is the last function called in a test by each node. It is
	// responsible for collecting any data from the node and/or running any
	// retrospective tests or benchmarks.
	Retro(runenv *runtime.RunEnv, initCtx *run.InitContext) error
}

var _ Role = (*Leader)(nil)

// Leader is the role for the leader node in a test. It is responsible for
// creating the genesis block and distributing it to all nodes.
type Leader struct {
	*ConsensusNode
	ConfigGenerator func(runenv *runtime.RunEnv) (Config, error)
	TopologyFn      TopologyFn
}

// Plan is the method that creates and distributes the genesis, configurations,
// and keys for all of the other nodes in the network.
func (l *Leader) Plan(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	err := ConfigureNetwork(l.ctx, runenv, initCtx)
	if err != nil {
		return err
	}

	cfg, err := l.ConfigGenerator(runenv)
	if err != nil {
		return err
	}

	cfg.Nodes, err = l.TopologyFn(runenv, cfg.Nodes)
	if err != nil {
		return err
	}

	return PublishConfig(l.ctx, initCtx, cfg)
}

func (l *Leader) Execute(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	baseDir, err := l.ConsensusNode.Init(homeDir)
	if err != nil {
		return err
	}
	l.ConsensusNode.StartNode(l.ctx, baseDir)
	return nil
}

// Retro collects standard data from the leader node and saves it as a file.
// This data includes the block times, rounds required to reach consensus, and
// the block sizes.
func (l *Leader) Retro(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	defer l.ConsensusNode.Stop()

	return nil
}

func StandardConfig(runenv *runtime.RunEnv) (Config, error) {
	nodeCount := runenv.RunParams.TestGroupInstanceCount

	// Create validators for each instance.
	vals := make([]genesis.Validator, 0, nodeCount)
	vals = append(vals, genesis.NewDefaultValidator("leader"))
	for i := 0; i < nodeCount-1; i++ {
		vals = append(vals, genesis.NewDefaultValidator(fmt.Sprintf("follower-%d", i)))
	}
	g := genesis.NewDefaultGenesis().
		WithValidators(vals...).
		WithAccounts(genesis.NewAccounts(999999999999999999, TxSimAccountName)...)

	gDoc, err := g.Export()
	if err != nil {
		return Config{}, nil
	}

	genDocBytes, err := cmtjson.MarshalIndent(gDoc, "", "  ")
	if err != nil {
		return Config{}, err
	}

	nodes := make(map[string]NodeConfig)
	for i, val := range vals {

		nodes[val.Name] = NodeConfig{
			Role:        "validator",
			Validator:   true,
			Seed:        false,
			Name:        val.Name,
			StartHeight: 0,
			Keys: KeySet{
				NetworkKey:      val.NetworkKey,
				ConsensusKey:    val.ConsensusKey,
				AccountMnemonic: g.Accounts()[i].Mnemonic,
			},
			CmtConfig: app.DefaultConsensusConfig(),
			AppConfig: app.DefaultAppConfig(),
			P2PID:     ValidatorP2PID(val, "192.168", i),
		}
	}

	cfg := Config{
		ChainID: g.ChainID,
		Genesis: genDocBytes,
		Nodes:   nodes,
	}

	return cfg, nil
}

func ValidatorP2PID(val genesis.Validator, baseID string, sequence int) string {
	nodeID := string(p2p.PubKeyToID(val.NetworkKey.PubKey()))
	return fmt.Sprintf("%s@%s:26656", nodeID, calculateIPAddresses("198.168", []int{})[0])
}
