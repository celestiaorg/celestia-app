package e2e

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/celestiaorg/knuu/pkg/knuu"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/types"
)

const (
	rpcPort       = 26657
	p2pPort       = 26656
	grpcPort      = 9090
	dockerSrcURL  = "ghcr.io/celestiaorg/celestia-app"
	secp256k1Type = "secp256k1"
	ed25519Type   = "ed25519"
)

type Node struct {
	Name           string
	Version        string
	StartHeight    int64
	InitialPeers   []string
	SignerKey      crypto.PrivKey
	NetworkKey     crypto.PrivKey
	AccountKey     crypto.PrivKey
	SelfDelegation int64
	Instance       *knuu.Instance

	rpcProxyPort  int
	grpcProxyPort int
}

func NewNode(
	name, version string,
	startHeight, selfDelegation int64,
	peers []string,
	signerKey, networkKey, accountKey crypto.PrivKey,
) (*Node, error) {
	instance, err := knuu.NewInstance(name)
	if err != nil {
		return nil, err
	}
	err = instance.SetImage(fmt.Sprintf("%s:%s", dockerSrcURL, version))
	if err != nil {
		return nil, err
	}
	if err := instance.AddPortTCP(rpcPort); err != nil {
		return nil, err
	}
	if err := instance.AddPortTCP(p2pPort); err != nil {
		return nil, err
	}
	if err := instance.AddPortTCP(grpcPort); err != nil {
		return nil, err
	}
	err = instance.SetMemory("200Mi", "200Mi")
	if err != nil {
		return nil, err
	}
	err = instance.SetCPU("300m")
	if err != nil {
		return nil, err
	}
	err = instance.AddVolume("/root/.celestia-app", "1Gi")
	if err != nil {
		return nil, err
	}
	err = instance.SetArgs("start", "--home=/root/.celestia-app", "--rpc.laddr=tcp://0.0.0.0:26657")
	if err != nil {
		return nil, err
	}
	return &Node{
		Name:           name,
		Instance:       instance,
		Version:        version,
		StartHeight:    startHeight,
		InitialPeers:   peers,
		SignerKey:      signerKey,
		NetworkKey:     networkKey,
		AccountKey:     accountKey,
		SelfDelegation: selfDelegation,
	}, nil
}

func (n *Node) Init(genesis types.GenesisDoc) error {
	// Initialize file directories
	rootDir := os.TempDir()
	nodeDir := filepath.Join(rootDir, n.Name)
	for _, dir := range []string{
		filepath.Join(nodeDir, "config"),
		filepath.Join(nodeDir, "data"),
	} {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("error creating directory %s: %w", dir, err)
		}
	}

	// Create and write the config file
	cfg, err := MakeConfig(n)
	if err != nil {
		return fmt.Errorf("making config: %w", err)
	}
	configFilePath := filepath.Join(nodeDir, "config", "config.toml")
	config.WriteConfigFile(configFilePath, cfg)

	// Store the genesis file
	genesisFilePath := filepath.Join(nodeDir, "config", "genesis.json")
	err = genesis.SaveAs(genesisFilePath)
	if err != nil {
		return fmt.Errorf("saving genesis: %w", err)
	}

	// Create the app.toml file
	appConfig, err := MakeAppConfig(n)
	if err != nil {
		return fmt.Errorf("making app config: %w", err)
	}
	appConfigFilePath := filepath.Join(nodeDir, "config", "app.toml")
	serverconfig.WriteConfigFile(appConfigFilePath, appConfig)

	// Store the node key for the p2p handshake
	nodeKeyFilePath := filepath.Join(nodeDir, "config", "node_key.json")
	err = (&p2p.NodeKey{PrivKey: n.NetworkKey}).SaveAs(nodeKeyFilePath)
	if err != nil {
		return err
	}

	// Store the validator signer key for consensus
	pvKeyPath := filepath.Join(nodeDir, "config", "priv_validator_key.json")
	pvStatePath := filepath.Join(nodeDir, "data", "priv_validator_state.json")
	(privval.NewFilePV(n.SignerKey, pvKeyPath, pvStatePath)).Save()

	err = n.Instance.AddFile(configFilePath, filepath.Join("/root/.celestia-app/config", "config.toml"), "0:0")
	if err != nil {
		return fmt.Errorf("adding config file: %w", err)
	}

	err = n.Instance.AddFile(genesisFilePath, filepath.Join("/root/.celestia-app/config", "genesis.json"), "0:0")
	if err != nil {
		return fmt.Errorf("adding genesis file: %w", err)
	}

	err = n.Instance.AddFile(appConfigFilePath, filepath.Join("/root/.celestia-app/config", "app.toml"), "0:0")
	if err != nil {
		return fmt.Errorf("adding app config file: %w", err)
	}

	err = n.Instance.AddFile(pvKeyPath, filepath.Join("/root/.celestia-app/config", "priv_validator_key.json"), "0:0")
	if err != nil {
		return fmt.Errorf("adding priv_validator_key file: %w", err)
	}

	err = n.Instance.AddFile(pvStatePath, filepath.Join("/root/.celestia-app/data", "priv_validator_state.json"), "0:0")
	if err != nil {
		return fmt.Errorf("adding priv_validator_state file: %w", err)
	}

	err = n.Instance.AddFile(nodeKeyFilePath, filepath.Join("/root/.celestia-app/config", "node_key.json"), "0:0")
	if err != nil {
		return fmt.Errorf("adding node_key file: %w", err)
	}

	return n.Instance.Commit()
}

// Address returns a P2P endpoint address for the node.
func (n Node) AddressP2P(withID bool) string {
	ip, err := n.Instance.GetIP()
	if err != nil {
		panic(err)
	}
	addr := fmt.Sprintf("%v:%d", ip, p2pPort)
	if withID {
		addr = fmt.Sprintf("%x@%v", n.NetworkKey.PubKey().Address().Bytes(), addr)
	}
	return addr
}

// Address returns an RPC endpoint address for the node.
func (n Node) AddressRPC() string {
	ip, err := n.Instance.GetIP()
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%v:%d", ip, rpcPort)
}

// Address returns a GRPC endpoint address for the node.
func (n Node) AddressGRPC() string {
	ip, err := n.Instance.GetIP()
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%v:%d", ip, grpcPort)
}

func (n Node) IsValidator() bool {
	return n.SelfDelegation != 0
}

func (n Node) Client() (*http.HTTP, error) {
	return http.New(fmt.Sprintf("http://127.0.0.1:%v", n.rpcProxyPort), "/websocket")
}

func (n *Node) Start() error {
	if err := n.Instance.Start(); err != nil {
		return err
	}
	rpcProxyPort, err := n.Instance.PortForwardTCP(rpcPort)
	if err != nil {
		return err
	}
	grpcProxyPort, err := n.Instance.PortForwardTCP(grpcPort)
	if err != nil {
		return err
	}
	n.rpcProxyPort = rpcProxyPort
	n.grpcProxyPort = grpcProxyPort
	return nil
}
