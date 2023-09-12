package network

import (
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/test/genesis"
	cmtjson "github.com/tendermint/tendermint/libs/json"
	"github.com/tendermint/tendermint/p2p"
	"github.com/testground/sdk-go/runtime"
)

const (
	ValidatorsParam = "validators"
	FullNodesParam  = "full_nodes"
	HaltHeightParam = "halt_height"
	PexParam        = "pex"
	SeedNodeParam   = "seed_node"
)

type Params struct {
	Validators  int
	FullNodes   int
	HaltHeight  int
	Timeout     time.Duration
	Pex         bool
	TopologyFns []TopologyFn
}

func ParseParams(runenv *runtime.RunEnv) (*Params, error) {
	var err error
	p := &Params{}
	p.Validators = runenv.IntParam(ValidatorsParam)
	p.FullNodes = runenv.IntParam(FullNodesParam)
	p.HaltHeight = runenv.IntParam(HaltHeightParam)
	p.TopologyFns, err = GetTopologyFns(runenv)
	if err != nil {
		return nil, err
	}
	p.Pex = runenv.BooleanParam(PexParam)
	return p, p.ValidateBasic()
}

func (p *Params) ValidateBasic() error {
	if p.Validators < 1 {
		return errors.New("invalid number of validators")
	}
	if p.FullNodes < 0 {
		return errors.New("invalid number of full nodes")
	}

	return nil
}

func (p *Params) NodeCount() int {
	return p.FullNodes + p.Validators
}

func (p *Params) GenerateConfig() (Config, error) {
	// Create validators for each instance.
	vals := make([]genesis.Validator, 0, p.Validators)

	// use the global sequence as the name for each validator or node
	// note: the leader is always the first validator so "0".
	for i := 0; i < p.Validators; i++ {
		vals = append(vals, genesis.NewDefaultValidator(NodeID(i)))
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

	nodes := make([]NodeConfig, 0, p.NodeCount())
	for i, val := range vals {
		nodes[i] = NodeConfig{
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

	for _, top := range p.TopologyFns {
		nodes, err = top(nodes)
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
	return fmt.Sprintf("%s@%s:26656", nodeID, calculateIPAddresses(baseID, sequence))
}
