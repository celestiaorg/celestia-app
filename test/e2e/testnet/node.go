//nolint:staticcheck
package testnet

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
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

	rpcProxyHost string
	// FIXME: This does not work currently with the reverse proxy
	// grpcProxyHost  string
	traceProxyHost string
	args           []string
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

// PullBlockSummaryTraces retrieves the block summary traces from a node.
// It will save them to the provided path.
func (n *Node) PullBlockSummaryTraces(path string) ([]trace.Event[schema.BlockSummary], error,
) {
	addr := n.AddressTracing()
	log.Info().Str("Address", addr).Msg("Pulling block summary traces")

	err := trace.GetTable(addr, schema.BlockSummary{}.Table(), path)
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
	disableBBR bool,
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
	if disableBBR {
		args = append(args, "--force-no-bbr")
	}
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
		args:           args,
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
// This returns the proxy host that can be used to communicate with the node
func (n Node) AddressRPC() string {
	return n.rpcProxyHost
}

// FIXME: This does not work currently with the reverse proxy
// // AddressGRPC returns a GRPC endpoint address for the node.
// // This returns the proxy host that can be used to communicate with the node
// func (n Node) AddressGRPC() string {
// 	return n.grpcProxyHost
// }

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
	return n.traceProxyHost
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
	log.Debug().Str("RPC Address", n.AddressRPC()).Msg("Creating HTTP client for node")
	return http.New(n.AddressRPC(), "/websocket")
}

func (n *Node) Start() error {
	if err := n.StartAsync(); err != nil {
		return err
	}

	return n.WaitUntilStartedAndForwardPorts()
}

func (n *Node) StartAsync() error {
	return n.Instance.StartAsync()
}

func (n *Node) WaitUntilStartedAndForwardPorts() error {
	if err := n.Instance.WaitInstanceIsRunning(); err != nil {
		return err
	}

	err, rpcProxyHost := n.Instance.AddHost(rpcPort)
	if err != nil {
		return err
	}
	n.rpcProxyHost = rpcProxyHost

	// FIXME: This does not work currently with the reverse proxy
	// err, grpcProxyHost := n.Instance.AddHost(grpcPort)
	// if err != nil {
	// 	return err
	// }
	// n.grpcProxyHost = grpcProxyHost

	err, traceProxyHost := n.Instance.AddHost(tracingPort)
	if err != nil {
		return err
	}
	n.traceProxyHost = traceProxyHost

	return nil
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

func (n *Node) Upgrade(version string, disableBBR bool) error {
	if err := n.Instance.SetImageInstant(DockerImageName(version)); err != nil {
		return err
	}

	//if disableBBR {
	//	// FIXME: This does not work currently since the node is not in state 'Preparing' or 'Committed'
	//	//n.args = append(n.args, "--force-no-bbr")
	//	//err := n.Instance.SetArgs(n.args...)
	//	//if err != nil {
	//	//	return fmt.Errorf("cannot pass --force-no-bbr flag: %w", err)
	//	//}
	//}

	return n.Instance.WaitInstanceIsRunning()
}

func DockerImageName(version string) string {
	return fmt.Sprintf("%s:%s", dockerSrcURL, version)
}

func (n *Node) GetArgs() []string {
	return n.args
}

func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}
