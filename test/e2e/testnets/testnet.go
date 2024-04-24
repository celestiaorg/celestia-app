package testnets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
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
	manifest  TestManifest
}

func New(name string, seed int64, grafana *GrafanaInfo, manifest TestManifest) (*Testnet,
	error) {
	identifier := fmt.Sprintf("%s_%s", name, time.Now().Format("20060102_150405"))
	if err := knuu.InitializeWithScope(identifier); err != nil {
		return nil, err
	}

	// if a GovMaxSquareSize is provided in manifest, set the blob params in the genesis
	g := genesis.NewDefaultGenesis()
	if manifest.GovMaxSquareSize != 0 {
		blobGenState := blobtypes.DefaultGenesis()
		blobGenState.Params.GovMaxSquareSize = uint64(manifest.GovMaxSquareSize)
		ecfg := encoding.MakeConfig(app.ModuleBasics)
		g.WithModifiers(genesis.SetBlobParams(ecfg.Codec, blobGenState.Params))
	}
	// if a MaxBlockBytes is provided in the manifest, set the consensus params in the genesis
	if manifest.MaxBlockBytes != 0 {
		g.ConsensusParams.Block.MaxBytes = manifest.MaxBlockBytes
	}
	return &Testnet{
		seed:     seed,
		nodes:    make([]*Node, 0),
		genesis:  g.WithChainID(manifest.ChainID),
		keygen:   newKeyGenerator(seed),
		grafana:  grafana,
		manifest: manifest,
	}, nil
}

func (t *Testnet) SetConsensusParams(params *tmproto.ConsensusParams) {
	t.genesis.WithConsensusParams(params)
}

func (t *Testnet) CreateGenesisNode(version string, selfDelegation, upgradeHeight int64, resources Resources) error {
	signerKey := t.keygen.Generate(ed25519Type)
	networkKey := t.keygen.Generate(ed25519Type)
	node, err := NewNode(fmt.Sprintf("val%d", len(t.nodes)), version, 0, selfDelegation, nil, signerKey, networkKey, upgradeHeight, resources, t.grafana)
	if err != nil {
		return err
	}
	if err := t.genesis.AddValidator(node.GenesisValidator()); err != nil {
		return err
	}
	t.nodes = append(t.nodes, node)
	return nil
}

func (t *Testnet) CreateGenesisNodes() error {
	for i := 0; i < t.manifest.Validators; i++ {
		if err := t.CreateGenesisNode(t.manifest.CelestiaAppVersion,
			t.manifest.SelfDelegation, t.manifest.UpgradeHeight,
			t.manifest.ValidatorResource); err != nil {
			return err
		}
	}
	return nil
}

func (t *Testnet) CreateTxClients(
	grpcEndpoints []string,
) error {
	for i, grpcEndpoint := range grpcEndpoints {
		name := fmt.Sprintf("txsim%d", i)
		err := t.CreateTxClient(name, t.manifest.TxClientVersion, t.manifest.BlobSequences,
			t.manifest.BlobSizes, t.manifest.TxClientsResource, grpcEndpoint)
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
		sequences, blobRange, 1, resources, txsimRootDir)
	if err != nil {
		log.Err(err).
			Str("name", name).
			Msg("error creating txsim")
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

	err = txsim.Instance.Commit()
	if err != nil {
		log.Err(err).
			Str("name", name).
			Msg("error committing txsim")
		return err
	}

	t.txClients = append(t.txClients, txsim)
	return nil
}

func (t *Testnet) StartTxClients() error {
	for _, txsim := range t.txClients {
		err := txsim.Instance.Start()
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
	node, err := NewNode(fmt.Sprintf("val%d", len(t.nodes)), version, startHeight, 0, nil, signerKey, networkKey, upgradeHeight, resources, t.grafana)
	if err != nil {
		return err
	}
	t.nodes = append(t.nodes, node)
	return nil
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

		err := node.Init(t, genesis, peers)
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
				log.Err(err).
					Str("name", node.Name).
					Msg("node  failed to stop")
				continue
			}
			if err := node.Instance.WaitInstanceIsStopped(); err != nil {
				log.Err(err).
					Str("name", node.Name).
					Msg("node  failed to stop")
				continue
			}
		}
		if node.Instance.IsInState(knuu.Started, knuu.Stopped) {
			err := node.Instance.Destroy()
			if err != nil {
				log.Err(err).
					Str("name", node.Name).
					Msg("node  failed to cleanup")
			}
		}
	}
	// stop and cleanup txsim
	for _, txsim := range t.txClients {
		if txsim.Instance.IsInState(knuu.Started) {
			err := txsim.Instance.Stop()
			if err != nil {
				log.Err(err).
					Str("name", txsim.Name).
					Msg("txsim failed to stop")
			}
			err = txsim.Instance.WaitInstanceIsStopped()
			if err != nil {
				log.Err(err).
					Str("name", txsim.Name).
					Msg("txsim failed to stop")
			}
			err = txsim.Instance.Destroy()
			if err != nil {
				log.Err(err).
					Str("name", txsim.Name).
					Msg("txsim failed to cleanup")
			}
		}
	}
}

func (t *Testnet) Node(i int) *Node {
	return t.nodes[i]
}
