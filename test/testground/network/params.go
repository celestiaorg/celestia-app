package network

import (
	"errors"
	"fmt"
	"time"

	mrand "math/rand"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/test/genesis"
	"github.com/tendermint/tendermint/crypto/ed25519"
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

func (p *Params) StandardConfig(statuses []Status) (Config, error) {
	// set the global configs for each node
	cmtcfg := app.DefaultConsensusConfig()
	cmtcfg.Instrumentation.PrometheusListenAddr = "0.0.0.0:26660"
	cmtcfg.Instrumentation.Prometheus = true
	cmtcfg.P2P.PexReactor = p.Pex

	vals := make([]genesis.Validator, 0)
	accs := make([]genesis.Account, 0)
	networkKeys := make([]ed25519.PrivKey, 0, len(statuses))
	r := mrand.New(mrand.NewSource(time.Now().UnixNano()))

	nodes := make([]NodeConfig, p.NodeCount())
	for i, status := range statuses {
		networkKeys = append(networkKeys, genesis.GenerateEd25519(genesis.NewSeed(r)))
		nodeName := fmt.Sprintf("%d", status.GlobalSequence)

		consensusKey := ed25519.GenPrivKey()
		switch status.NodeType {
		case "validators":
			val := genesis.NewDefaultValidator(nodeName)
			consensusKey = val.ConsensusKey
			vals = append(vals, val)
		case "full_nodes":
			accs = append(accs, genesis.NewAccounts(999999999999999999, nodeName)...)
		}

		nodes[i] = NodeConfig{
			NodeType:    status.NodeType,
			Name:        fmt.Sprintf("%d", status.GlobalSequence),
			StartHeight: 0,
			HaltHeight:  p.HaltHeight,
			Keys: KeySet{
				NetworkKey:   networkKeys[i],
				ConsensusKey: consensusKey,
			},
			CmtConfig: cmtcfg,
			AppConfig: app.DefaultAppConfig(),
			P2PID:     peerID(status, networkKeys[i]),
		}
	}

	g := genesis.NewDefaultGenesis().
		WithValidators(vals...).
		WithAccounts()

	nodes = setMnenomics(g.Accounts(), nodes)

	gDoc, err := g.Export()
	if err != nil {
		return Config{}, nil
	}

	genDocBytes, err := cmtjson.MarshalIndent(gDoc, "", "  ")
	if err != nil {
		return Config{}, err
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

func peerID(status Status, networkKey ed25519.PrivKey) string {
	nodeID := string(p2p.PubKeyToID(networkKey.PubKey()))
	return fmt.Sprintf("%s@%s:26656", nodeID, status.IP)
}

// todo: have a better way to just generate the key here and set it in the account
func setMnenomics(accs []genesis.Account, nodeCfgs []NodeConfig) []NodeConfig {
	for i, cfg := range nodeCfgs {
		for _, acc := range accs {
			if acc.Name == cfg.Name {
				cfg.Keys.AccountMnemonic = acc.Mnemonic
				nodeCfgs[i] = cfg
			}
		}
	}
	return nodeCfgs
}
