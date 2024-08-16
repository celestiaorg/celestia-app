//nolint:staticcheck
package testnet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/rs/zerolog/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

type Testnet struct {
	seed      int64
	nodes     []*Node
	genesis   *genesis.Genesis
	keygen    *keyGenerator
	grafana   *GrafanaInfo
	txClients []*TxSim
}

func New(name string, seed int64, grafana *GrafanaInfo, chainID string,
	genesisModifiers ...genesis.Modifier) (
	*Testnet, error,
) {
	identifier := fmt.Sprintf("%s_%s", name, time.Now().Format("20060102_150405"))
	if err := knuu.InitializeWithScope(identifier); err != nil {
		return nil, err
	}

	return &Testnet{
		seed:    seed,
		nodes:   make([]*Node, 0),
		genesis: genesis.NewDefaultGenesis().WithChainID(chainID).WithModifiers(genesisModifiers...),
		keygen:  newKeyGenerator(seed),
		grafana: grafana,
	}, nil
}

func (t *Testnet) SetConsensusParams(params *tmproto.ConsensusParams) {
	t.genesis.WithConsensusParams(params)
}

func (t *Testnet) SetConsensusMaxBlockSize(size int64) {
	t.genesis.ConsensusParams.Block.MaxBytes = size
}

func (t *Testnet) CreateGenesisNode(version string, selfDelegation, upgradeHeight int64, resources Resources) error {
	signerKey := t.keygen.Generate(ed25519Type)
	networkKey := t.keygen.Generate(ed25519Type)
	node, err := NewNode(fmt.Sprintf("val%d", len(t.nodes)), version, 0,
		selfDelegation, nil, signerKey, networkKey, upgradeHeight, resources,
		t.grafana)
	if err != nil {
		return err
	}
	if err := t.genesis.NewValidator(node.GenesisValidator()); err != nil {
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

func (t *Testnet) CreateTxClients(version string,
	sequences int,
	blobRange string,
	blobPerSequence int,
	resources Resources,
	grpcEndpoints []string,
) error {
	for i, grpcEndpoint := range grpcEndpoints {
		name := fmt.Sprintf("txsim%d", i)
		err := t.CreateTxClient(name, version, sequences,
			blobRange, blobPerSequence, resources, grpcEndpoint)
		if err != nil {
			log.Err(err).Str("name", name).
				Str("grpc endpoint", grpcEndpoint).
				Msg("txsim creation failed")
			return err
		}
		log.Info().
			Str("name", name).
			Str("grpc endpoint", grpcEndpoint).
			Msg("txsim created")
	}
	return nil
}

// CreateTxClient creates a txsim node and sets it up
// name: name of the txsim knuu instance
// version: version of the txsim docker image to be pulled from the registry
// specified by txsimDockerSrcURL
// seed: seed for the txsim
// sequences: number of sequences to be run by the txsim
// blobRange: range of blob sizes to be used by the txsim in bytes
// pollTime: time in seconds between each sequence
// resources: resources to be allocated to the txsim
// grpcEndpoint: grpc endpoint of the node to which the txsim will connect and send transactions
func (t *Testnet) CreateTxClient(name,
	version string,
	sequences int,
	blobRange string,
	blobPerSequence int,
	resources Resources,
	grpcEndpoint string,
) error {
	// create an account, and store it in a temp directory and add the account as genesis account to
	// the testnet
	txsimKeyringDir := filepath.Join(os.TempDir(), name)
	log.Info().
		Str("name", name).
		Str("directory", txsimKeyringDir).
		Msg("txsim keyring directory created")
	_, err := t.CreateAccount(name, 1e16, txsimKeyringDir)
	if err != nil {
		return err
	}

	// Create a txsim node using the key stored in the txsimKeyringDir
	txsim, err := CreateTxClient(name, version, grpcEndpoint, t.seed,
		sequences, blobRange, blobPerSequence, 1, resources, txsimRootDir)
	if err != nil {
		log.Err(err).
			Str("name", name).
			Msg("error creating txsim")
		return err
	}
	err = txsim.Instance.Commit()
	if err != nil {
		log.Err(err).
			Str("name", name).
			Msg("error committing txsim")
		return err
	}

	// copy over the keyring directory to the txsim instance
	err = txsim.Instance.AddFolder(txsimKeyringDir, txsimRootDir, "10001:10001")
	if err != nil {
		log.Err(err).
			Str("directory", txsimKeyringDir).
			Str("name", name).
			Msg("error adding keyring dir to txsim")
		return err
	}

	t.txClients = append(t.txClients, txsim)
	return nil
}

func (t *Testnet) StartTxClients() error {
	for _, txsim := range t.txClients {
		err := txsim.Instance.StartWithoutWait()
		if err != nil {
			log.Err(err).
				Str("name", txsim.Name).
				Msg("txsim failed to start")
			return err
		}
		log.Info().
			Str("name", txsim.Name).
			Msg("txsim started")
	}
	// wait for txsims to start
	for _, txsim := range t.txClients {
		err := txsim.Instance.WaitInstanceIsRunning()
		if err != nil {
			return fmt.Errorf("txsim %s failed to run: %w", txsim.Name, err)
		}

	}
	return nil
}

// CreateAccount creates an account and adds it to the
// testnet genesis. The account is created with the given name and tokens and
// is persisted in the given txsimKeyringDir.
// If txsimKeyringDir is an empty string, an in-memory keyring is created.
func (t *Testnet) CreateAccount(name string, tokens int64, txsimKeyringDir string) (keyring.Keyring, error) {
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...).Codec
	var kr keyring.Keyring
	var err error
	// if no keyring directory is specified, create an in-memory keyring
	if txsimKeyringDir == "" {
		kr = keyring.NewInMemory(cdc)
	} else { // create a keyring with the specified directory
		kr, err = keyring.New(app.Name, keyring.BackendTest,
			txsimKeyringDir, nil, cdc)
		if err != nil {
			return nil, err
		}
	}
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
		Name:    name,
	})
	if err != nil {
		return nil, err
	}

	log.Info().
		Str("name", name).
		Str("pk", pk.String()).
		Msg("txsim account created and added to genesis")
	return kr, nil
}

func (t *Testnet) CreateNode(version string, startHeight, upgradeHeight int64, resources Resources) error {
	signerKey := t.keygen.Generate(ed25519Type)
	networkKey := t.keygen.Generate(ed25519Type)
	node, err := NewNode(fmt.Sprintf("val%d", len(t.nodes)), version,
		startHeight, 0, nil, signerKey, networkKey, upgradeHeight, resources,
		t.grafana)
	if err != nil {
		return err
	}
	t.nodes = append(t.nodes, node)
	return nil
}

func (t *Testnet) Setup(configOpts ...Option) error {
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

		err := node.Init(genesis, peers, configOpts...)
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

// FIXME: This does not work currently with the reverse proxy
// func (t *Testnet) GRPCEndpoints() []string {
// 	grpcEndpoints := make([]string, len(t.nodes))
// 	for idx, node := range t.nodes {
// 		grpcEndpoints[idx] = node.AddressGRPC()
// 	}
// 	return grpcEndpoints
// }

// RemoteGRPCEndpoints retrieves the gRPC endpoint addresses of the
// testnet's validator nodes.
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

func (t *Testnet) GetGenesisValidators() []genesis.Validator {
	validators := make([]genesis.Validator, len(t.nodes))
	for i, node := range t.nodes {
		validators[i] = node.GenesisValidator()
	}
	return validators
}

// RemoteRPCEndpoints retrieves the RPC endpoint addresses of the testnet's
// validator nodes.
func (t *Testnet) RemoteRPCEndpoints() ([]string, error) {
	rpcEndpoints := make([]string, len(t.nodes))
	for idx, node := range t.nodes {
		grpcEP, err := node.RemoteAddressRPC()
		if err != nil {
			return nil, err
		}
		rpcEndpoints[idx] = grpcEP
	}
	return rpcEndpoints, nil
}

// WaitToSync waits for the started nodes to sync with the network and move
// past the genesis block.
func (t *Testnet) WaitToSync() error {
	genesisNodes := make([]*Node, 0)
	for _, node := range t.nodes {
		if node.StartHeight == 0 {
			genesisNodes = append(genesisNodes, node)
		}
	}
	for _, node := range genesisNodes {
		log.Info().Str("name", node.Name).Msg(
			"waiting for node to sync")
		client, err := node.Client()
		if err != nil {
			return fmt.Errorf("failed to initialize client for node %s: %w", node.Name, err)
		}
		for i := 0; i < 10; i++ {
			resp, err := client.Status(context.Background())
			if err == nil {
				if resp.SyncInfo.LatestBlockHeight > 0 {
					log.Info().Int("attempts", i).Str("name", node.Name).Msg(
						"node has synced")
					break
				}
			} else {
				err = errors.New("error getting status")
			}
			if i == 9 {
				return fmt.Errorf("failed to start node %s: %w", node.Name, err)
			}
			log.Info().Str("name", node.Name).Int("attempt", i).Msg(
				"node is not synced yet, waiting...")
			time.Sleep(time.Duration(i) * time.Second)
		}
	}
	return nil
}

// StartNodes starts the testnet nodes and forwards the ports.
// It does not wait for the nodes to produce blocks.
// For that, use WaitToSync.
func (t *Testnet) StartNodes() error {
	genesisNodes := make([]*Node, 0)
	for _, node := range t.nodes {
		if node.StartHeight == 0 {
			genesisNodes = append(genesisNodes, node)
		}
	}
	// start genesis nodes asynchronously
	for _, node := range genesisNodes {
		err := node.StartAsync()
		if err != nil {
			return fmt.Errorf("node %s failed to start: %w", node.Name, err)
		}
	}
	log.Info().Msg("forwarding ports for genesis nodes")
	// wait for instances to be running
	for _, node := range genesisNodes {
		err := node.WaitUntilStartedAndForwardPorts()
		if err != nil {
			return fmt.Errorf("node %s failed to start: %w", node.Name, err)
		}
	}
	return nil
}

func (t *Testnet) Start() error {
	// start nodes and forward ports
	err := t.StartNodes()
	if err != nil {
		return err
	}
	// wait for nodes to sync
	log.Info().Msg("waiting for genesis nodes to sync")
	err = t.WaitToSync()
	if err != nil {
		return err
	}

	return t.StartTxClients()
}

func (t *Testnet) Cleanup() {
	// cleanup txsim
	for _, txsim := range t.txClients {
		err := txsim.Instance.Destroy()
		if err != nil {
			log.Err(err).
				Str("name", txsim.Name).
				Msg("txsim failed to cleanup")
		}
	}
	// cleanup nodes
	for _, node := range t.nodes {
		err := node.Instance.Destroy()
		if err != nil {
			log.Err(err).
				Str("name", node.Name).
				Msg("node failed to cleanup")
		}
	}
}

func (t *Testnet) Node(i int) *Node {
	return t.nodes[i]
}

func (t *Testnet) Nodes() []*Node {
	return t.nodes
}
