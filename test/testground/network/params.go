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
	ValidatorsParam        = "validators"
	FullNodesParam         = "full_nodes"
	HaltHeightParam        = "halt_height"
	PexParam               = "pex"
	SeedNodeParam          = "seed_node"
	BlobSequencesParam     = "blob_sequences"
	BlobSizesParam         = "blob_sizes"
	BlobsPerSeqParam       = "blobs_per_sequence"
	TimeoutCommitParam     = "timeout_commit"
	TimeoutProposeParam    = "timeout_propose"
	InboundPeerCountParam  = "inbound_peer_count"
	OutboundPeerCountParam = "outbound_peer_count"
	GovMaxSquareSizeParam  = "gov_max_square_size"
	MaxBlockBytesParam     = "max_block_bytes"
)

type Params struct {
	Validators        int
	FullNodes         int
	HaltHeight        int
	Timeout           time.Duration
	Pex               bool
	TopologyFns       []TopologyFn
	PerPeerBandwidth  int
	BlobsPerSeq       int
	BlobSequences     int
	BlobSizes         int
	InboundPeerCount  int
	OutboundPeerCount int
	GovMaxSquareSize  int
	MaxBlockBytes     int
	TimeoutCommit     time.Duration
	TimeoutPropose    time.Duration
}

func ParseParams(runenv *runtime.RunEnv) (*Params, error) {
	var err error
	p := &Params{}

	p.Validators = runenv.IntParam(ValidatorsParam)

	p.FullNodes = runenv.IntParam(FullNodesParam)

	p.HaltHeight = runenv.IntParam(HaltHeightParam)

	p.BlobSequences = runenv.IntParam(BlobSequencesParam)

	p.BlobSizes = runenv.IntParam(BlobSizesParam)

	p.BlobsPerSeq = runenv.IntParam(BlobsPerSeqParam)

	p.InboundPeerCount = runenv.IntParam(InboundPeerCountParam)

	p.OutboundPeerCount = runenv.IntParam(OutboundPeerCountParam)

	p.GovMaxSquareSize = runenv.IntParam(GovMaxSquareSizeParam)

	p.MaxBlockBytes = runenv.IntParam(MaxBlockBytesParam)

	p.Timeout, err = time.ParseDuration(runenv.StringParam(TimeoutCommitParam))
	if err != nil {
		return nil, err
	}

	p.TimeoutCommit, err = time.ParseDuration(runenv.StringParam(TimeoutCommitParam))
	if err != nil {
		return nil, err
	}

	p.TimeoutPropose, err = time.ParseDuration(runenv.StringParam(TimeoutProposeParam))
	if err != nil {
		return nil, err
	}

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
	cmtcfg.P2P.SendRate = int64(p.PerPeerBandwidth)
	cmtcfg.P2P.RecvRate = int64(p.PerPeerBandwidth)
	cmtcfg.Consensus.TimeoutCommit = p.TimeoutCommit
	cmtcfg.Consensus.TimeoutPropose = p.TimeoutPropose
	cmtcfg.TxIndex.Indexer = "kv"

	vals := make([]genesis.Validator, 0)
	accs := make([]genesis.Account, 0)
	networkKeys := make([]ed25519.PrivKey, 0, len(statuses))
	r := mrand.New(mrand.NewSource(time.Now().UnixNano()))

	nodes := []NodeConfig{}
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
		default:
			return Config{}, fmt.Errorf("unknown node type %s", status.NodeType)
		}

		nodes = append(nodes, NodeConfig{
			Status:      status,
			NodeType:    status.NodeType,
			Name:        nodeName,
			StartHeight: 0,
			HaltHeight:  p.HaltHeight,
			Keys: KeySet{
				NetworkKey:   networkKeys[i],
				ConsensusKey: consensusKey,
			},
			CmtConfig: *cmtcfg,
			AppConfig: *app.DefaultAppConfig(),
			P2PID:     peerID(status, networkKeys[i]),
		})
	}

	g := genesis.NewDefaultGenesis().
		WithValidators(vals...).
		WithAccounts(accs...)

	gDoc, err := g.Export()
	if err != nil {
		return Config{}, nil
	}

	genDocBytes, err := cmtjson.MarshalIndent(gDoc, "", "  ")
	if err != nil {
		return Config{}, err
	}

	nodes, err = setMnemomics(g.Accounts(), nodes)
	if err != nil {
		return Config{}, err
	}

	for _, node := range nodes {
		if node.Keys.AccountMnemonic == "" {
			return Config{}, fmt.Errorf("mnemonic not found for account %s", node.Name)
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

func peerID(status Status, networkKey ed25519.PrivKey) string {
	nodeID := string(p2p.PubKeyToID(networkKey.PubKey()))
	return fmt.Sprintf("%s@%s:26656", nodeID, status.IP)
}

func setMnemomics(accs []genesis.Account, nodeCfgs []NodeConfig) ([]NodeConfig, error) {
	accountMap := make(map[string]genesis.Account)
	for _, acc := range accs {
		accountMap[acc.Name] = acc
	}
	if len(accountMap) != len(accs) {
		return nil, fmt.Errorf("duplicate account names found")
	}
	if len(nodeCfgs) > len(accountMap) {

		return nil, fmt.Errorf("node count and account count mismatch: accounts %d node configs %d", len(accountMap), len(nodeCfgs))
	}
	for i, cfg := range nodeCfgs {
		if acc, ok := accountMap[cfg.Name]; ok {
			if acc.Mnemonic == "" {
				return nil, fmt.Errorf("mnemonic not found for account %s", acc.Name)
			}
			nodeCfgs[i].Keys.AccountMnemonic = acc.Mnemonic
			continue
		}
		return nil, fmt.Errorf("account not found for node %s", cfg.Name)
	}
	return nodeCfgs, nil
}
