package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/rs/zerolog/log"
)

type Testnet struct {
	seed            int64
	nodes           []*Node
	genesisAccounts []*Account
	keygen          *keyGenerator
	txSimNode       *TxSim
}

func New(name string, seed int64) (*Testnet, error) {
	identifier := fmt.Sprintf("%s_%s", name, time.Now().Format("20060102_150405"))
	if err := knuu.InitializeWithIdentifier(identifier); err != nil {
		return nil, err
	}

	return &Testnet{
		seed:            seed,
		nodes:           make([]*Node, 0),
		genesisAccounts: make([]*Account, 0),
		keygen:          newKeyGenerator(seed),
	}, nil
}

func (t *Testnet) CreateGenesisNode(version string, selfDelegation, upgradeHeight int64, resources Resources) error {
	signerKey := t.keygen.Generate(ed25519Type)
	networkKey := t.keygen.Generate(ed25519Type)
	accountKey := t.keygen.Generate(secp256k1Type)
	node, err := NewNode(fmt.Sprintf("val%d", len(t.nodes)), version, 0, selfDelegation, nil, signerKey, networkKey, accountKey, upgradeHeight, resources)
	if err != nil {
		return err
	}
	t.nodes = append(t.nodes, node)
	return nil
}

func (t *Testnet) CreateGenesisNodes(num int, version string, selfDelegation, upgradeHeight int64, resources Resources) error {
	for i := 0; i < num; i++ {
		if err := t.CreateGenesisNode(version, selfDelegation, upgradeHeight, resources); err != nil {
			return err
		}
	}
	return nil
}

// CreateAndSetupTxSimNode creates a txsim node and sets it up
// name: name of the txsim knuu instance
// version: version of the txsim docker image to be pulled from the registry
// specified by txsimDockerSrcURL
// seed: seed for the txsim
// sequences: number of sequences to be run by the txsim
// blobRange: range of blob sizes to be used by the txsim in bytes
// pollTime: time in seconds between each sequence
// resources: resources to be allocated to the txsim
// grpcEndpoint: grpc endpoint of the node to which the txsim will connect and send transactions
func (t *Testnet) CreateAndSetupTxSimNode(name,
	version string,
	seed int,
	sequences int,
	blobRange string,
	pollTime int,
	resources Resources,
	grpcEndpoint string) error {
	// create an account,
	//store it in a temp directory and add the account as genesis account to
	//the testnet
	txsimKeyringDir := filepath.Join(os.TempDir(), "txsim")
	_, err := t.CreateAndAddAccountToGenesis("alice", 1e12, txsimKeyringDir)
	if err != nil {
		return err
	}

	// Create a txsim node using the key stored in the txsimKeyringDir
	txsim, err := CreateTxSimNode(name, version, grpcEndpoint, seed, sequences,
		blobRange, pollTime, resources, txsimRootDir)
	if err != nil {
		return err
	}
	// copy over the keyring directory to the txsim instance
	err = txsim.Instance.AddFolder(txsimKeyringDir, txsimRootDir, "10001:10001")
	if err != nil {
		return err
	}

	err = txsim.Instance.Commit()
	if err != nil {
		return err
	}

	t.txSimNode = txsim
	return nil
}

func (t *Testnet) StartTxSimNode() error {
	err := t.txSimNode.Instance.Start()
	if err != nil {
		return err
	}
	return t.txSimNode.Instance.WaitInstanceIsRunning()
}

// CreateAndAddAccountToGenesis creates an account and adds it to the
// testnet genesis. The account is created with the given name and tokens and
// is persisted in the given txsimKeyringDir.
func (t *Testnet) CreateAndAddAccountToGenesis(name string, tokens int64, txsimKeyringDir string) (keyring.Keyring, error) {
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...).Codec
	kr, err := keyring.New(app.Name, keyring.BackendTest, txsimKeyringDir, nil, cdc)
	if err != nil {
		return nil, err
	}
	key, _, err := kr.NewMnemonic(name, keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		return nil, err
	}

	pk, err := key.GetPubKey()
	if err != nil {
		return nil, err
	}
	t.genesisAccounts = append(t.genesisAccounts, &Account{
		PubKey:        pk,
		InitialTokens: tokens,
	})
	return kr, nil
}

func (t *Testnet) CreateNode(version string, startHeight, upgradeHeight int64, resources Resources) error {
	signerKey := t.keygen.Generate(ed25519Type)
	networkKey := t.keygen.Generate(ed25519Type)
	accountKey := t.keygen.Generate(secp256k1Type)
	node, err := NewNode(fmt.Sprintf("val%d", len(t.nodes)), version, startHeight, 0, nil, signerKey, networkKey, accountKey, upgradeHeight, resources)
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
	t.genesisAccounts = append(t.genesisAccounts, &Account{
		PubKey:        pk,
		InitialTokens: tokens,
	})
	return kr, nil
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
		// nodes are initialized with the addresses of all other
		// nodes in their addressbook
		peers := make([]string, 0, len(t.nodes)-1)
		for _, peer := range t.nodes {
			if peer.Name != node.Name {
				peers = append(peers, peer.AddressP2P(true))
			}
		}

		err = node.Init(genesis, peers)
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
func (t *Testnet) RemoteGRPCEndpoints() ([]string, error) {
	grpcEndpoints := make([]string, len(t.nodes))
	for idx, node := range t.nodes {
		grpcEP, err := node.RemoteAddressGRPC()
		if err != nil {
			return nil, err
		}
		grpcEndpoints[idx] = grpcEP
	}
	return grpcEndpoints, nil
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
		}
		err := node.Instance.Destroy()
		if err != nil {
			log.Err(err).Msg(fmt.Sprintf("node %s failed to cleanup", node.Name))
		}
	}
	// stop and cleanup txsim
	if t.txSimNode.Instance != nil && t.txSimNode.Instance.IsInState(knuu.Started) {
		err := t.txSimNode.Instance.Stop()
		if err != nil {
			log.Err(err).Msg(fmt.Sprintf("txsim %s failed to stop", t.txSimNode.Name))
		}
		err = t.txSimNode.Instance.Destroy()
		if err != nil {
			log.Err(err).Msg(fmt.Sprintf("txsim %s failed to cleanup", t.txSimNode.Name))
		}
	}
}

func (t *Testnet) Node(i int) *Node {
	return t.nodes[i]
}
