//nolint:staticcheck
package testnet

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/libs/trace"
	"github.com/cometbft/cometbft/libs/trace/schema"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cometbft/cometbft/types"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/celestiaorg/knuu/pkg/instance"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/celestiaorg/knuu/pkg/sidecars/netshaper"
	"github.com/celestiaorg/knuu/pkg/sidecars/observability"

	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
)

const (
	rpcPort        = 26657
	p2pPort        = 26656
	grpcPort       = 9090
	prometheusPort = 26660
	tracingPort    = 26661
	//dockerSrcURL   = "ghcr.io/01builders/celestia-app" // TODO: revert to celestiaorg
	dockerSrcURL  = "docker.io/chatton/celestia-app" // TODO: revert to celestiaorg
	secp256k1Type = "secp256k1"
	ed25519Type   = "ed25519"
	remoteRootDir = "/home/celestia/.celestia-app"
	txsimRootDir  = "/home/celestia"
)

type Node struct {
	Name           string
	Version        string
	StartHeight    int64
	InitialPeers   []string
	SignerKey      crypto.PrivKey
	NetworkKey     crypto.PrivKey
	SelfDelegation int64
	Instance       *instance.Instance
	sidecars       []instance.SidecarManager
	netShaper      *netshaper.NetShaper // a reference to the netshaper sidecar

	rpcProxyHost string
	// FIXME: This does not work currently with the reverse proxy
	// grpcProxyHost  string
	traceProxyHost string

	logger *log.Logger
}

// PullRoundStateTraces retrieves the round state traces from a node.
// It will save them to the provided path.
func (n *Node) PullRoundStateTraces(path string) ([]trace.Event[schema.RoundState], error) {
	addr := n.AddressTracing()
	n.logger.Println("Pulling round state traces", "address", addr)

	err := trace.GetTable(addr, schema.RoundState{}.Table(), path)
	if err != nil {
		return nil, fmt.Errorf("getting table: %w", err)
	}
	return nil, nil
}

// PullBlockSummaryTraces retrieves the block summary traces from a node.
// It will save them to the provided path.
func (n *Node) PullBlockSummaryTraces(path string) ([]trace.Event[schema.BlockSummary], error) {
	addr := n.AddressTracing()
	n.logger.Println("Pulling block summary traces", "address", addr)

	err := trace.GetTable(addr, schema.BlockSummary{}.Table(), path)
	if err != nil {
		return nil, fmt.Errorf("getting table: %w", err)
	}
	return nil, nil
}

// Resources defines the resource requirements for a Node.
type Resources struct {
	// MemoryRequest specifies the initial memory allocation for the Node.
	MemoryRequest resource.Quantity
	// MemoryLimit specifies the maximum memory allocation for the Node.
	MemoryLimit resource.Quantity
	// CPU specifies the CPU allocation for the Node.
	CPU resource.Quantity
	// Volume specifies the storage volume allocation for the Node.
	Volume resource.Quantity
}

func NewNode(
	ctx context.Context,
	logger *log.Logger,
	name string,
	version string,
	startHeight int64,
	selfDelegation int64,
	peers []string,
	signerKey crypto.PrivKey,
	networkKey crypto.PrivKey,
	upgradeHeightV2 int64,
	resources Resources,
	grafana *GrafanaInfo,
	kn *knuu.Knuu,
	disableBBR bool,
) (*Node, error) {
	knInstance, err := kn.NewInstance(name)
	if err != nil {
		return nil, err
	}
	err = knInstance.Build().SetImage(ctx, DockerImageName(version))
	if err != nil {
		return nil, err
	}

	for _, port := range []int{rpcPort, p2pPort, grpcPort, tracingPort} {
		if err := knInstance.Network().AddPortTCP(port); err != nil {
			return nil, err
		}
	}

	var sidecars []instance.SidecarManager
	if grafana != nil {
		obsySc := observability.New()

		// add support for metrics
		if err := obsySc.SetPrometheusEndpoint(prometheusPort, fmt.Sprintf("knuu-%s", kn.Scope), "1m"); err != nil {
			return nil, fmt.Errorf("setting prometheus endpoint: %w", err)
		}
		if err := obsySc.SetJaegerEndpoint(14250, 6831, 14268); err != nil {
			return nil, fmt.Errorf("error setting jaeger endpoint: %v", err)
		}
		if err := obsySc.SetOtlpExporter(grafana.Endpoint, grafana.Username, grafana.Token); err != nil {
			return nil, fmt.Errorf("error setting otlp exporter: %v", err)
		}
		if err := obsySc.SetJaegerExporter("jaeger-collector.jaeger-cluster.svc.cluster.local:14250"); err != nil {
			return nil, fmt.Errorf("error setting jaeger exporter: %v", err)
		}
		sidecars = append(sidecars, obsySc)
	}
	err = knInstance.Resources().SetMemory(resources.MemoryRequest, resources.MemoryLimit)
	if err != nil {
		return nil, err
	}
	err = knInstance.Resources().SetCPU(resources.CPU)
	if err != nil {
		return nil, err
	}
	err = knInstance.Storage().AddVolumeWithOwner(remoteRootDir, resources.Volume, 10001)
	if err != nil {
		return nil, err
	}
	args := []string{"start", fmt.Sprintf("--home=%s", remoteRootDir), "--rpc.laddr=tcp://0.0.0.0:26657"}
	if disableBBR {
		args = append(args, "--force-no-bbr")
	}
	if upgradeHeightV2 != 0 {
		args = append(args, fmt.Sprintf("--v2-upgrade-height=%d", upgradeHeightV2))
	}

	if err := knInstance.Build().SetArgs(args...); err != nil {
		return nil, err
	}

	return &Node{
		Name:           name,
		Instance:       knInstance,
		Version:        version,
		StartHeight:    startHeight,
		InitialPeers:   peers,
		SignerKey:      signerKey,
		NetworkKey:     networkKey,
		SelfDelegation: selfDelegation,
		sidecars:       sidecars,
		logger:         logger,
	}, nil
}

func (n *Node) EnableNetShaper() {
	n.netShaper = netshaper.New()
	n.sidecars = append(n.sidecars, n.netShaper)
}

func (n *Node) SetLatencyAndJitter(latency, jitter int64) error {
	if n.netShaper == nil {
		return fmt.Errorf("netshaper is not enabled")
	}
	return n.netShaper.SetLatencyAndJitter(latency, jitter)
}

func (n *Node) Init(ctx context.Context, genesis *types.GenesisDoc, peers []string, configOptions ...Option) error {
	if len(peers) == 0 {
		return fmt.Errorf("no peers provided")
	}

	// Initialize file directories
	tmpDir, err := os.MkdirTemp("", "e2e_test_")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	nodeDir := filepath.Join(tmpDir, n.Name)
	n.logger.Println("Creating validator's config and data directories", "name", n.Name, "directory", nodeDir)
	for _, dir := range []string{
		filepath.Join(nodeDir, "config"),
		filepath.Join(nodeDir, "data"),
	} {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("error creating directory %s: %w", dir, err)
		}
	}

	// Create and write the config file
	cfg, err := MakeConfig(n, peers, configOptions...)
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

	if err := n.Instance.Build().Commit(ctx); err != nil {
		return fmt.Errorf("committing instance: %w", err)
	}

	for _, sc := range n.sidecars {
		if err := n.Instance.Sidecars().Add(ctx, sc); err != nil {
			return fmt.Errorf("adding sidecar: %w", err)
		}
	}

	if err = n.Instance.Storage().AddFolder(nodeDir, remoteRootDir, "10001:10001"); err != nil {
		return fmt.Errorf("copying over node %s directory: %w", n.Name, err)
	}
	return nil
}

// AddressP2P returns a P2P endpoint address for the node. This is used for
// populating the address book. This will look something like:
// 3314051954fc072a0678ec0cbac690ad8676ab98@61.108.66.220:26656
func (n Node) AddressP2P(withID bool) string {
	hostName := n.Instance.Network().HostName()
	addr := fmt.Sprintf("%v:%d", hostName, p2pPort)
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
	hostName := n.Instance.Network().HostName()
	return fmt.Sprintf("%s:%d", hostName, grpcPort), nil
}

// RemoteAddressRPC retrieves the RPC endpoint address of a node within the cluster.
func (n Node) RemoteAddressRPC() (string, error) {
	hostName := n.Instance.Network().HostName()
	return fmt.Sprintf("%s:%d", hostName, rpcPort), nil
}

func (n Node) AddressTracing() string {
	return n.traceProxyHost
}

func (n Node) RemoteAddressTracing() (string, error) {
	hostName := n.Instance.Network().HostName()
	return fmt.Sprintf("http://%s:26661", hostName), nil
}

func (n Node) IsValidator() bool {
	return n.SelfDelegation != 0
}

func (n Node) Client() (*http.HTTP, error) {
	n.logger.Println("Creating HTTP client for node", "rpc_address", n.AddressRPC())
	return http.New(n.AddressRPC(), "/websocket")
}

func (n *Node) Start(ctx context.Context) error {
	if err := n.StartAsync(ctx); err != nil {
		return err
	}

	return n.WaitUntilStartedAndCreateProxy(ctx)
}

func (n *Node) StartAsync(ctx context.Context) error {
	return n.Instance.Execution().StartAsync(ctx)
}

func (n *Node) WaitUntilStartedAndCreateProxy(ctx context.Context) error {
	if err := n.Instance.Execution().WaitInstanceIsRunning(ctx); err != nil {
		return err
	}

	// TODO: It is recommended to use AddHostWithReadyCheck for the proxy
	rpcProxyHost, err := n.Instance.Network().AddHost(ctx, rpcPort)
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

	// TODO: It is recommended to use AddHostWithReadyCheck for the proxy
	traceProxyHost, err := n.Instance.Network().AddHost(ctx, tracingPort)
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

func (n *Node) Upgrade(ctx context.Context, version string) error {
	if err := n.Instance.Execution().Stop(ctx); err != nil {
		return err
	}

	if err := n.Instance.Execution().SetImage(ctx, DockerImageName(version)); err != nil {
		return err
	}

	// New set of args can be set here
	// Or/and the start command can also be set here

	if err := n.Instance.Build().Commit(ctx); err != nil {
		return err
	}
	return n.Instance.Execution().Start(ctx)
}

func DockerImageName(version string) string {
	return fmt.Sprintf("%s:%s", dockerSrcURL, version)
}
