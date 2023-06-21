package e2e

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
)

type Testnet struct {
	seed            int64
	nodes           []*Node
	genesisAccounts []*GenesisAccount
	sequences       []*txsim.Sequence
	keygen          *keyGenerator
}

func New(seed int64) *Testnet {
	return &Testnet{
		seed:            seed,
		nodes:           make([]*Node, 0),
		genesisAccounts: make([]*GenesisAccount, 0),
		keygen:          newKeyGenerator(seed),
	}
}

func (t *Testnet) CreateGenesisNode(version string, selfDelegation int64) error {
	signerKey := t.keygen.Generate(ed25519Type)
	networkKey := t.keygen.Generate(ed25519Type)
	accountKey := t.keygen.Generate(secp256k1Type)
	node, err := NewNode(fmt.Sprintf("val%d", len(t.nodes)), version, 0, selfDelegation, nil, signerKey, networkKey, accountKey)
	if err != nil {
		return err
	}
	t.nodes = append(t.nodes, node)
	return nil
}

func (t *Testnet) CreateGenesisNodes(num int, version string, selfDelegation int64) error {
	for i := -0; i < num; i++ {
		if err := t.CreateGenesisNode(version, selfDelegation); err != nil {
			return err
		}
	}
	return nil
}

func (t *Testnet) CreateNode(version string, startHeight int64) error {
	signerKey := t.keygen.Generate(ed25519Type)
	networkKey := t.keygen.Generate(ed25519Type)
	accountKey := t.keygen.Generate(secp256k1Type)
	node, err := NewNode(fmt.Sprintf("val%d", len(t.nodes)), version, startHeight, 0, nil, signerKey, networkKey, accountKey)
	if err != nil {
		return err
	}
	t.nodes = append(t.nodes, node)
	return nil
}

func (t *Testnet) CreateGenesisAccount(name string, tokens int64) (keyring.Keyring, error) {
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
	t.genesisAccounts = append(t.genesisAccounts, &GenesisAccount{
		PubKey:        pk,
		InitialTokens: tokens,
	})
	return kr, nil
}

func (t *Testnet) SetSequences(sequences []*txsim.Sequence) {
	t.sequences = sequences
}

func (t *Testnet) Setup() error {
	genesisNodes := make([]*Node, 0)
	for _, node := range t.nodes {
		if node.StartHeight == 0 && node.SelfDelegation > 0 {
			genesisNodes = append(genesisNodes, node)
		}
	}
	genesis, err := MakeGenesis(genesisNodes, t.genesisAccounts)
	if err != nil {
		return err
	}
	for _, node := range t.nodes {
		err = node.Init(genesis)
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
	return nil
}
