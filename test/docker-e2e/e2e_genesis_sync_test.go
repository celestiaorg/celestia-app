package docker_e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/docker/port"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	mobyclient "github.com/moby/moby/client"

	"celestiaorg/celestia-app/test/docker-e2e/networks"
)

const (
	// genesisSyncTimeout bounds copying the genesis file into the container,
	// InitChain on the full genesis, peer discovery, and block syncing the
	// first blocks from live peers.
	genesisSyncTimeout = 15 * time.Minute
	// Start the shorter peer deadline only once RPC responds, allowing genesis
	// initialization to finish before diagnosing external peer availability.
	archivePeerTimeout = 3 * time.Minute
	// mainnetV2UpgradeHeight is the height at which Mainnet Beta upgraded from
	// app version 1 to 2. The embedded v3 binary needs it to replay v1 blocks.
	mainnetV2UpgradeHeight = 2371495
	// maxDiagnosticLogBytes caps the container log tail printed when a node
	// fails to start.
	maxDiagnosticLogBytes = 20_000
)

// TestGenesisSyncMocha starts a full node from the Mocha genesis file and
// verifies it processes genesis and block syncs the first block from live peers.
func (s *CelestiaTestSuite) TestGenesisSyncMocha() {
	s.runGenesisSync(networks.NewMochaConfig())
}

// TestGenesisSyncMainnet starts a full node from the Mainnet Beta genesis file
// and verifies it processes genesis and block syncs the first block from live peers.
func (s *CelestiaTestSuite) TestGenesisSyncMainnet() {
	s.runGenesisSync(networks.NewMainnetConfig(), fmt.Sprintf("--v2-upgrade-height=%d", mainnetV2UpgradeHeight))
}

// runGenesisSync starts a single full node with the real genesis file of the
// given network, block syncs from seeds and archive peers, and asserts the
// node reaches block 1. State sync stays disabled (tastora default) so the
// node must process genesis and the first blocks itself.
func (s *CelestiaTestSuite) runGenesisSync(netCfg *networks.Config, extraStartArgs ...string) {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()

	dockerCfg, err := networks.NewConfig(netCfg, s.client, s.network)
	s.Require().NoError(err, "failed to create %s config", netCfg.Name)

	// WithAdditionalStartArgs replaces the builder defaults, so --force-no-bbr
	// must be passed again. Peers go in as flags because tastora overwrites
	// persistent_peers in config.toml with the chain's own internal nodes.
	startArgs := append([]string{"--force-no-bbr"}, extraStartArgs...)
	if netCfg.Seeds != "" {
		startArgs = append(startArgs, "--p2p.seeds="+netCfg.Seeds)
	}
	peers := s.archivePeers(ctx, netCfg)
	s.Require().NotEmpty(peers, "no %s archive peer to block sync from", netCfg.Name)
	startArgs = append(startArgs, "--p2p.persistent_peers="+peers)

	chain, err := networks.NewChainBuilder(t, netCfg, dockerCfg).
		WithAdditionalStartArgs(startArgs...).
		WithBlockWaitTimeout(genesisSyncTimeout).
		WithNodes(cosmos.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
			Build(),
		).
		Build(ctx)
	s.Require().NoError(err, "failed to create %s chain", netCfg.Name)

	t.Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	t.Logf("Starting %s node from genesis", netCfg.Name)
	startTime := time.Now()

	// Start returns only after the node reports two blocks, so a successful
	// start means genesis was processed and the first blocks were synced.
	nodes := chain.GetNodes()
	s.Require().Len(nodes, 1, "expected exactly one node")
	probe := s.genesisSyncProbe(nodes[0].Name(), peers)
	if err := startWithArchivePeerCheck(ctx, chain.Start, probe, archivePeerTimeout, 5*time.Second); err != nil {
		s.logNodeDiagnostics(ctx, chain.GetNodes())
		s.Require().NoError(err, "%s genesis sync failed (archive peers: %s)", netCfg.Name, peers)
	}
	t.Logf("%s node reached its first blocks after %s", netCfg.Name, time.Since(startTime))

	client, err := nodes[0].GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	err = s.WaitForSync(ctx, client, time.Minute, func(info rpctypes.SyncInfo) bool {
		return info.LatestBlockHeight >= 1
	})
	s.Require().NoError(err, "%s node did not report height >= 1", netCfg.Name)

	height := int64(1)
	block, err := client.Block(ctx, &height)
	s.Require().NoError(err, "failed to fetch block 1 from %s node", netCfg.Name)
	s.Require().Equal(netCfg.ChainID, block.Block.ChainID, "block 1 has unexpected chain ID")
}

// genesisSyncProbe uses its own RPC client. Tastora assigns the node's client
// during Start, so reading GetRPCClient concurrently with Start would race.
func (s *CelestiaTestSuite) genesisSyncProbe(nodeName, peers string) func(context.Context) (bool, bool) {
	archiveIDs := make(map[string]bool)
	for peer := range strings.SplitSeq(peers, ",") {
		id, _, _ := strings.Cut(peer, "@")
		archiveIDs[id] = true
	}
	return func(ctx context.Context) (ready, connected bool) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		inspect, err := s.client.ContainerInspect(ctx, nodeName, mobyclient.ContainerInspectOptions{})
		if err != nil {
			return false, false
		}
		addr := port.GetForHost(inspect.Container, "26657/tcp")
		if addr == "" {
			return false, false
		}
		client, err := networks.NewClient("http://" + addr)
		if err != nil {
			return false, false
		}
		status, err := client.Status(ctx)
		if err != nil {
			return false, false
		}
		// Processing block 1 proves that a peer served genesis history, even
		// if it disconnected or was discovered via a seed rather than our list.
		if status.SyncInfo.LatestBlockHeight >= 1 {
			return true, true
		}
		netInfo, err := client.NetInfo(ctx)
		if err != nil {
			return true, false
		}
		for _, peer := range netInfo.Peers {
			if archiveIDs[string(peer.NodeInfo.DefaultNodeID)] {
				return true, true
			}
		}
		return true, false
	}
}

// archivePeers returns the peers to block sync genesis from: the addresses the
// configured RPCs report for themselves, ahead of the hardcoded fallback. The
// hardcoded list rots whenever an operator turns on pruning or moves a port,
// and the node gives no sign of it — it stalls at height 0 with a healthy peer
// count until the test times out — so the live addresses are preferred.
func (s *CelestiaTestSuite) archivePeers(ctx context.Context, netCfg *networks.Config) string {
	t := s.T()
	fetched, err := networks.FetchArchivePeers(ctx, netCfg)
	if err != nil {
		t.Logf("falling back to the hardcoded %s archive peers: %v", netCfg.Name, err)
		return netCfg.Peers
	}
	peers := networks.MergePeers(fetched, netCfg.Peers)
	t.Logf("%s archive peers: %s", netCfg.Name, peers)
	return peers
}

// logNodeDiagnostics logs each node's sync status, peer count, and the tail of
// its container logs so a failed start can be diagnosed from CI output alone.
func (s *CelestiaTestSuite) logNodeDiagnostics(ctx context.Context, nodes []tastoratypes.ChainNode) {
	t := s.T()
	for _, n := range nodes {
		node, ok := n.(*cosmos.ChainNode)
		if !ok {
			continue
		}
		if client, err := node.GetRPCClient(); err == nil && client != nil {
			if status, err := client.Status(ctx); err != nil {
				t.Logf("%s status error: %v", node.Name(), err)
			} else {
				t.Logf("%s status: height=%d catching_up=%t", node.Name(), status.SyncInfo.LatestBlockHeight, status.SyncInfo.CatchingUp)
			}
			if netInfo, err := client.NetInfo(ctx); err == nil {
				// Name the peers: a stalled sync with a healthy peer count
				// means the node connected to pruned peers only, which the
				// count alone does not show.
				peers := make([]string, 0, len(netInfo.Peers))
				for _, p := range netInfo.Peers {
					peers = append(peers, fmt.Sprintf("%s@%s (%s)", p.NodeInfo.DefaultNodeID, p.RemoteIP, p.NodeInfo.Moniker))
				}
				t.Logf("%s peers: %d %v", node.Name(), netInfo.NPeers, peers)
			}
		}
		logs := s.containerLogs(ctx, node.Name())
		if len(logs) > maxDiagnosticLogBytes {
			logs = logs[len(logs)-maxDiagnosticLogBytes:]
		}
		t.Logf("%s logs (tail):\n%s", node.Name(), logs)
	}
}
