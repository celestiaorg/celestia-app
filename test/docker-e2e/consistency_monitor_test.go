package docker_e2e

import (
	"context"
	"time"

	"github.com/celestiaorg/tastora/framework/docker/cosmos"

	"celestiaorg/celestia-app/test/docker-e2e/internal"
)

const (
	monitorInterval     = 2 * time.Second
	monitorStallTimeout = 3 * time.Minute // longer than the 2 minute binary swap budget in UpgradeChain
	// monitorRPCTimeout bounds each RPC so one blocked node cannot hold up a poll.
	monitorRPCTimeout = 10 * time.Second
)

// rpcNode reads heights and blocks from a node's CometBFT RPC. Status and
// Block are answered from the block store, so they are safe to call while the
// multiplexer swaps binaries.
type rpcNode struct{ node *cosmos.ChainNode }

func (n rpcNode) Name() string { return n.node.Name() }

func (n rpcNode) Height(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, monitorRPCTimeout)
	defer cancel()
	status, err := n.node.Client.Status(ctx)
	if err != nil {
		return 0, err
	}
	return status.SyncInfo.LatestBlockHeight, nil
}

func (n rpcNode) Block(ctx context.Context, height int64) (internal.BlockInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, monitorRPCTimeout)
	defer cancel()
	block, err := n.node.Client.Block(ctx, &height)
	if err != nil {
		return internal.BlockInfo{}, err
	}
	return internal.BlockInfo{BlockHash: block.BlockID.Hash.String(), AppHash: block.Block.AppHash.String()}, nil
}

func rpcNodes(nodes cosmos.ChainNodes) []internal.Node {
	out := make([]internal.Node, len(nodes))
	for i, n := range nodes {
		out[i] = rpcNode{node: n}
	}
	return out
}

// requireAgreement checks that every node has the same app hash and block hash at height.
func (s *CelestiaTestSuite) requireAgreement(ctx context.Context, nodes cosmos.ChainNodes, height int64) {
	s.Require().NoError(internal.CheckAgreement(ctx, rpcNodes(nodes), height))
}

// startConsistencyMonitor checks, until the test ends, that the nodes agree
// on the app hash and block hash of every height and that the chain does not
// stall. It also fails when a node does not answer for monitorStallTimeout.
// Use it only while no node is meant to halt, roll back or stay down.
func (s *CelestiaTestSuite) startConsistencyMonitor(ctx context.Context, nodes cosmos.ChainNodes) {
	m := internal.StartMonitor(ctx, rpcNodes(nodes), internal.MonitorConfig{
		Interval:     monitorInterval,
		StallTimeout: monitorStallTimeout,
	})
	t := s.T()
	t.Cleanup(func() {
		if err := m.Stop(); err != nil {
			t.Errorf("consistency monitor: %v", err)
			return
		}
		t.Logf("consistency monitor: nodes agreed up to height %d", m.CheckedHeight())
	})
}
