package testnet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/knuu/pkg/knuu"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/rs/zerolog/log"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/pkg/trace"
	"github.com/tendermint/tendermint/pkg/trace/schema"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/types"
)

const (
	rpcPort        = 26657
	p2pPort        = 26656
	grpcPort       = 9090
	prometheusPort = 26660
	tracingPort    = 26661
	dockerSrcURL   = "ghcr.io/celestiaorg/celestia-app"
	secp256k1Type  = "secp256k1"
	ed25519Type    = "ed25519"
	remoteRootDir  = "/home/celestia/.celestia-app"
	txsimRootDir   = "/home/celestia"
)

type Node struct {
	Name           string
	Version        string
	StartHeight    int64
	InitialPeers   []string
	SignerKey      crypto.PrivKey
	NetworkKey     crypto.PrivKey
	SelfDelegation int64
	Instance       *knuu.Instance

	rpcProxyPort   int
	grpcProxyPort  int
	traceProxyPort int
}

// PullRoundStateTraces retrieves the round state traces from a node.
// It will save them to the provided path.
func (n *Node) PullRoundStateTraces(path string) ([]trace.Event[schema.RoundState], error,
) {
	addr := n.AddressTracing()
	log.Info().Str("Address", addr).Msg("Pulling round state traces")

	err := trace.GetTable(addr, schema.RoundState{}.Table(), path)
	if err != nil {
		return nil, fmt.Errorf("getting table: %w", err)
	}
	return nil, nil
}

// Resources defines the resource requirements for a Node.
type Resources struct {
	// MemoryRequest specifies the initial memory allocation for the Node.
	MemoryRequest string
	// MemoryLimit specifies the maximum memory allocation for the Node.
	MemoryLimit string
	// CPU specifies the CPU allocation for the Node.
	CPU string
	// Volume specifies the storage volume allocation for the Node.
	Volume string
}

func NewNode(
	name, version string,
	startHeight, selfDelegation int64,
	peers []string,
	signerKey, networkKey crypto.PrivKey,
	upgradeHeight int64,
	resources Resources,
	grafana *GrafanaInfo,
) (*Node, error) {
	instance, err := knuu.NewInstance(name)
	if err != nil {
		return nil, err
	}
	err = instance.SetImage(DockerImageName(version))
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
	if err := instance.AddPortTCP(tracingPort); err != nil {
		return nil, err
	}

	if grafana != nil {
		// add support for metrics
		if err := instance.SetPrometheusEndpoint(prometheusPort, fmt.Sprintf("knuu-%s", knuu.Scope()), "1m"); err != nil {
			return nil, fmt.Errorf("setting prometheus endpoint: %w", err)
		}
		if err := instance.SetJaegerEndpoint(14250, 6831, 14268); err != nil {
			return nil, fmt.Errorf("error setting jaeger endpoint: %v", err)
		}
		if err := instance.SetOtlpExporter(grafana.Endpoint, grafana.Username, grafana.Token); err != nil {
			return nil, fmt.Errorf("error setting otlp exporter: %v", err)
		}
		if err := instance.SetJaegerExporter("jaeger-collector.jaeger-cluster.svc.cluster.local:14250"); err != nil {
			return nil, fmt.Errorf("error setting jaeger exporter: %v", err)
		}
	}
	err = instance.SetMemory(resources.MemoryRequest, resources.MemoryLimit)
	if err != nil {
		return nil, err
	}
	err = instance.SetCPU(resources.CPU)
	if err != nil {
		return nil, err
	}
	err = instance.AddVolumeWithOwner(remoteRootDir, resources.Volume, 10001)
	if err != nil {
		return nil, err
	}
	args := []string{"start", fmt.Sprintf("--home=%s", remoteRootDir), "--rpc.laddr=tcp://0.0.0.0:26657"}
	if upgradeHeight != 0 {
		args = append(args, fmt.Sprintf("--v2-upgrade-height=%d", upgradeHeight))
	}

	err = instance.SetArgs(args...)
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
		SelfDelegation: selfDelegation,
	}, nil
}

func (n *Node) Init(genesis *types.GenesisDoc, peers []string, configOptions ...Option) error {
	if len(peers) == 0 {
		return fmt.Errorf("no peers provided")
	}

	// Initialize file directories
	rootDir := os.TempDir()
	nodeDir := filepath.Join(rootDir, n.Name)
	log.Info().Str("name", n.Name).
		Str("directory", nodeDir).
		Msg("Creating validator's config and data directories")
	for _, dir := range []string{
		filepath.Join(nodeDir, "config"),
		filepath.Join(nodeDir, "data"),
	} {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("error creating directory %s: %w", dir, err)
		}
	}

	//if err := MakeTracePushConfig(filepath.Join(nodeDir, "config")); err != nil {
	//	return fmt.Errorf("error creating trace push config: %w", err)
	//}
	// Create and write the config file
	cfg, err := MakeConfig(n, configOptions...)
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

	err = os.Chmod(nodeKeyFilePath, 0o777)
	if err != nil {
		return fmt.Errorf("chmod node key: %w", err)
	}

	// Store the validator signer key for consensus
	pvKeyPath := filepath.Join(nodeDir, "config", "priv_validator_key.json")
	pvStatePath := filepath.Join(nodeDir, "data", "priv_validator_state.json")
	(privval.NewFilePV(n.SignerKey, pvKeyPath, pvStatePath)).Save()

	addrBookFile := filepath.Join(nodeDir, "config", "addrbook.json")
	err = WriteAddressBook(peers, addrBookFile)
	if err != nil {
		return fmt.Errorf("writing address book: %w", err)
	}

	err = n.Instance.Commit()
	if err != nil {
		return fmt.Errorf("committing instance: %w", err)
	}

	if err = n.Instance.AddFolder(nodeDir, remoteRootDir, "10001:10001"); err != nil {
		return fmt.Errorf("copying over node %s directory: %w", n.Name, err)
	}
	return nil
}

// AddressP2P returns a P2P endpoint address for the node. This is used for
// populating the address book. This will look something like:
// 3314051954fc072a0678ec0cbac690ad8676ab98@61.108.66.220:26656
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

// AddressRPC returns an RPC endpoint address for the node.
// This returns the local proxy port that can be used to communicate with the node
func (n Node) AddressRPC() string {
	return fmt.Sprintf("http://127.0.0.1:%d", n.rpcProxyPort)
}

// AddressGRPC returns a GRPC endpoint address for the node. This returns the
// local proxy port that can be used to communicate with the node
func (n Node) AddressGRPC() string {
	return fmt.Sprintf("127.0.0.1:%d", n.grpcProxyPort)
}

// RemoteAddressGRPC retrieves the gRPC endpoint address of a node within the cluster.
func (n Node) RemoteAddressGRPC() (string, error) {
	ip, err := n.Instance.GetIP()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", ip, grpcPort), nil
}

// RemoteAddressRPC retrieves the RPC endpoint address of a node within the cluster.
func (n Node) RemoteAddressRPC() (string, error) {
	ip, err := n.Instance.GetIP()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", ip, rpcPort), nil
}

func (n Node) AddressTracing() string {
	return fmt.Sprintf("http://127.0.0.1:%d", n.traceProxyPort)
}

func (n Node) RemoteAddressTracing() (string, error) {
	ip, err := n.Instance.GetIP()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://%s:26661", ip), nil
}

func (n Node) IsValidator() bool {
	return n.SelfDelegation != 0
}

func (n Node) Client() (*http.HTTP, error) {
	return http.New(n.AddressRPC(), "/websocket")
}

func (n *Node) Start() error {
	if err := n.StartAsync(); err != nil {
		return err
	}
	if err := n.WaitUntilStartedAndForwardPorts(); err != nil {
		return err
	}
	return nil
}

func (n *Node) StartAsync() error {
	if err := n.Instance.StartAsync(); err != nil {
		return err
	}
	return nil
}

func (n *Node) WaitUntilStartedAndForwardPorts() error {
	if err := n.Instance.WaitInstanceIsRunning(); err != nil {
		return err
	}
	return n.forwardPorts()
}

func (n *Node) GenesisValidator() genesis.Validator {
	return genesis.Validator{
		KeyringAccount: genesis.KeyringAccount{
			Name:          n.Name,
			InitialTokens: n.SelfDelegation,
		},
		ConsensusKey: n.SignerKey,
		NetworkKey:   n.NetworkKey,
		Stake:        n.SelfDelegation / 2,
	}
}

func (n *Node) Upgrade(version string) error {
	if err := n.Instance.SetImageInstant(DockerImageName(version)); err != nil {
		return err
	}

	if err := n.Instance.WaitInstanceIsRunning(); err != nil {
		return err
	}

	return n.forwardPorts()
}

func (n *Node) forwardPorts() error {
	rpcProxyPort, err := n.Instance.PortForwardTCP(rpcPort)
	if err != nil {
		return fmt.Errorf("forwarding port %d: %w", rpcPort, err)
	}

	grpcProxyPort, err := n.Instance.PortForwardTCP(grpcPort)
	if err != nil {
		return fmt.Errorf("forwarding port %d: %w", grpcPort, err)
	}

	traceProxyPort, err := n.Instance.PortForwardTCP(tracingPort)
	if err != nil {
		return fmt.Errorf("forwarding port %d: %w", tracingPort, err)
	}

	n.rpcProxyPort = rpcProxyPort
	n.grpcProxyPort = grpcProxyPort
	n.traceProxyPort = traceProxyPort

	return nil
}

func (n *Node) ForwardBitTwisterPort() error {
	fwdBtPort, err := n.Instance.PortForwardTCP(n.Instance.BitTwister.Port())
	if err != nil {
		return err
	}
	n.Instance.BitTwister.SetPort(fwdBtPort)
	n.Instance.BitTwister.SetNewClientByIPAddr("http://localhost")
	log.Info().Str("address", fmt.Sprintf("http://localhost:%d", fwdBtPort)).Msg("BitTwister is listening")
	return nil
}

func DockerImageName(version string) string {
	return fmt.Sprintf("%s:%s", dockerSrcURL, version)
}

func (n *Node) GetHeight(executor *knuu.Executor) (int64, error) {
	status, err := getStatus(executor, n.Instance)
	if err == nil {
		blockHeight, err := latestBlockHeightFromStatus(status)
		if err == nil {
			return blockHeight, nil
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 0, err
	}

	return 0, fmt.Errorf("error getting height: %w", err)
}
