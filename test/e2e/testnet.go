package e2e

import (
	"context"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/rs/zerolog/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

type Testnet struct {
	seed    int64
	nodes   []*Node
	genesis *genesis.Genesis
	keygen  *keyGenerator
	grafana *GrafanaInfo
}

func New(name string, seed int64, grafana *GrafanaInfo) (*Testnet, error) {
	identifier := fmt.Sprintf("%s_%s", name, time.Now().Format("20060102_150405"))
	if err := knuu.InitializeWithIdentifier(identifier); err != nil {
		return nil, err
	}

	return &Testnet{
		seed:    seed,
		nodes:   make([]*Node, 0),
		genesis: genesis.NewDefaultGenesis().WithChainID("test"),
		keygen:  newKeyGenerator(seed),
		grafana: grafana,
	}, nil
}

func (t *Testnet) SetConsensusParams(params *tmproto.ConsensusParams) {
	t.genesis.WithConsensusParams(params)
}

func (t *Testnet) CreateGenesisNode(version string, selfDelegation, upgradeHeight int64) error {
	signerKey := t.keygen.Generate(ed25519Type)
	networkKey := t.keygen.Generate(ed25519Type)
	node, err := NewNode(fmt.Sprintf("val%d", len(t.nodes)), version, 0, selfDelegation, nil, signerKey, networkKey, upgradeHeight, t.grafana)
	if err != nil {
		return err
	}
	if err := t.genesis.AddValidator(node.GenesisValidator()); err != nil {
		return err
	}
	t.nodes = append(t.nodes, node)
	return nil
}

func (t *Testnet) CreateGenesisNodes(num int, version string, selfDelegation, upgradeHeight int64) error {
	for i := 0; i < num; i++ {
		if err := t.CreateGenesisNode(version, selfDelegation, upgradeHeight); err != nil {
			return err
		}
	}
	return nil
}

func (t *Testnet) CreateNode(version string, startHeight, upgradeHeight int64) error {
	signerKey := t.keygen.Generate(ed25519Type)
	networkKey := t.keygen.Generate(ed25519Type)
	node, err := NewNode(fmt.Sprintf("val%d", len(t.nodes)), version, startHeight, 0, nil, signerKey, networkKey, upgradeHeight, t.grafana)
	if err != nil {
		return err
	}
	t.nodes = append(t.nodes, node)
	return nil
}

func (t *Testnet) CreateAccount(name string, tokens int64) (keyring.Keyring, error) {
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...).Codec
	kr := keyring.NewInMemory(cdc)
	key, _, err := kr.NewMnemonic(name, keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		return nil, err
	}
	pk, err := key.GetPubKey()
	if err != nil {
		return nil, err
	}
	err = t.genesis.AddAccount(genesis.Account{
		PubKey:  pk,
		Balance: tokens,
	})
	if err != nil {
		return nil, err
	}
	return kr, nil
}

func (t *Testnet) Setup() error {
	genesis, err := t.genesis.Export()
	if err != nil {
		return err
	}

	for _, node := range t.nodes {
		// nodes are initialized with the addresses of all other
		// nodes in their addressbook
		peers := make([]string, 0, len(t.nodes)-1)
		for _, peer := range t.nodes {
			if peer.Name != node.Name {
				peers = append(peers, peer.AddressP2P(true))
			}
		}

		err := node.Init(genesis, peers)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *Testnet) RPCEndpoints() []string {
	rpcEndpoints := make([]string, len(t.nodes))
	for idx, node := range t.nodes {
		rpcEndpoints[idx] = node.AddressRPC()
	}
	return rpcEndpoints
}

func (t *Testnet) GRPCEndpoints() []string {
	grpcEndpoints := make([]string, len(t.nodes))
	for idx, node := range t.nodes {
		grpcEndpoints[idx] = node.AddressGRPC()
	}
	return grpcEndpoints
}

func (t *Testnet) GetGenesisValidators() []genesis.Validator {
	validators := make([]genesis.Validator, len(t.nodes))
	for i, node := range t.nodes {
		validators[i] = node.GenesisValidator()
	}
	return validators
}

func (t *Testnet) Start() error {
	genesisNodes := make([]*Node, 0)
	for _, node := range t.nodes {
		if node.StartHeight == 0 {
			genesisNodes = append(genesisNodes, node)
		}
	}
	for _, node := range genesisNodes {
		err := node.Start()
		if err != nil {
			return fmt.Errorf("node %s failed to start: %w", node.Name, err)
		}
	}
	for _, node := range genesisNodes {
		client, err := node.Client()
		if err != nil {
			return fmt.Errorf("failed to initialized node %s: %w", node.Name, err)
		}
		for i := 0; i < 10; i++ {
			resp, err := client.Status(context.Background())
			if err != nil {
				return fmt.Errorf("node %s status response: %w", node.Name, err)
			}
			if resp.SyncInfo.LatestBlockHeight > 0 {
				break
			}
			if i == 9 {
				return fmt.Errorf("failed to start node %s", node.Name)
			}
			time.Sleep(time.Second)
		}
	}
	return nil
}

func (t *Testnet) Cleanup() {
	for _, node := range t.nodes {
		if node.Instance.IsInState(knuu.Started) {
			if err := node.Instance.Stop(); err != nil {
				log.Err(err).Msg(fmt.Sprintf("node %s failed to stop", node.Name))
				continue
			}
			if err := node.Instance.WaitInstanceIsStopped(); err != nil {
				log.Err(err).Msg(fmt.Sprintf("node %s failed to stop", node.Name))
				continue
			}
		}
		if node.Instance.IsInState(knuu.Started, knuu.Stopped) {
			err := node.Instance.Destroy()
			if err != nil {
				log.Err(err).Msg(fmt.Sprintf("node %s failed to cleanup", node.Name))
			}
		}
	}
}

func (t *Testnet) Node(i int) *Node {
	return t.nodes[i]
}
