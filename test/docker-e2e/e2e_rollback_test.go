package docker_e2e

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	addressutil "github.com/celestiaorg/tastora/framework/testutil/address"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	cometcfg "github.com/cometbft/cometbft/config"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	servercfg "github.com/cosmos/cosmos-sdk/server/config"
	mobyclient "github.com/moby/moby/client"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
)

const (
	rollbackHaltOffset = 30 // blocks from arming a halt height to the halt; covers the last validator's restart
	rollbackDepth      = 3  // executed heights rolled back in phase 2
	rollbackTimeout    = 5 * time.Minute
)

// TestRollback halts the validators with halt-height, rolls them back with
// the recovery script and checks that they rejoin the canonical chain.
func (s *CelestiaTestSuite) TestRollback() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()
	cfg := dockerchain.DefaultConfig(s.client, s.network)
	celestia, err := dockerchain.NewCelestiaChainBuilder(t, cfg).Build(ctx)
	s.Require().NoError(err, "failed to get chain")

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	t.Cleanup(func() {
		if err := celestia.Remove(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	// The full node never halts and is never rolled back; it serves the deleted blocks.
	err = celestia.AddNode(ctx, cosmos.NewChainNodeConfigBuilder().
		WithNodeType(tastoratypes.NodeTypeConsensusFull).
		WithPostInit(func(ctx context.Context, node *cosmos.ChainNode) error {
			// Tastora enables REST, which requires the Cosmos SDK gRPC server.
			return config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
				cfg.GRPC.Enable = true
			})
		}).
		Build())
	s.Require().NoError(err, "failed to add full node")

	validators := celestia.Validators
	fullNode := celestia.FullNodes[0]
	allNodes := celestia.Nodes()

	// Restarted validators get new IPs, so they must dial the full node themselves.
	fullNodePeer, err := addressutil.BuildInternalPeerAddressList(ctx, []*cosmos.ChainNode{fullNode})
	s.Require().NoError(err, "failed to get the full node peer address")

	// Operators may only have the image, so take the script from the binary.
	script, _, err := validators[0].Exec(ctx, []string{"celestia-appd", "recovery-script"}, nil)
	s.Require().NoError(err, "failed to print the recovery script from the image")
	s.Require().Contains(string(script), "Usage: recovery.sh", "unexpected recovery-script output")
	for _, v := range validators {
		s.Require().NoError(v.WriteFile(ctx, "recovery.sh", script), "failed to write the recovery script to %s", v.Name())
		// A snapshot every 2 heights leaves one above the phase 2 target for the script to delete.
		err := config.Modify(ctx, v, "config/app.toml", func(cfg *servercfg.Config) {
			cfg.StateSync.SnapshotInterval = 2
		})
		s.Require().NoError(err, "failed to enable snapshots on %s", v.Name())
	}

	err = s.WaitForSync(ctx, fullNode.Client, rollbackTimeout, func(info coretypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight > 0
	})
	s.Require().NoError(err, "full node failed to sync")
	startHeight, err := fullNode.Height(ctx)
	s.Require().NoError(err)

	haltHeight := s.armHaltHeight(ctx, validators, fullNode, fullNodePeer)
	t.Logf("Phase 1: halt at height %d", haltHeight)
	s.waitForHalt(ctx, validators, fullNode, haltHeight)
	s.requireHalted(ctx, fullNode, haltHeight)
	haltBlockHash := s.block(ctx, fullNode, haltHeight).BlockID.Hash.String()

	notExecuted := 0
	for _, v := range validators {
		_, err := s.rollback(ctx, v)
		if err == nil {
			t.Logf("%s lagged at the halt and was rolled back one height", v.Name())
			continue
		}
		s.Require().ErrorContains(err, fmt.Sprintf("block saved but not executed at height %d", haltHeight))
		notExecuted++

		s.recoveryRollback(ctx, v, haltHeight-1)
	}
	s.Require().GreaterOrEqual(notExecuted, 2, "expected the block-not-executed error on at least 2 validators")

	for _, v := range validators {
		s.restartWithHaltHeight(ctx, v, 0, fullNodePeer)
	}
	s.waitForHeight(ctx, allNodes, haltHeight+5)

	s.requireBlockHash(ctx, allNodes, haltHeight, haltBlockHash)
	s.requireBlockHash(ctx, allNodes, haltHeight+3, s.block(ctx, fullNode, haltHeight+3).BlockID.Hash.String())
	s.verifyAppHash(ctx, validators, fullNode, haltHeight+1)
	s.Require().NoError(s.CheckLiveness(ctx, celestia), "validator liveness check failed after phase 1")

	phase2HaltHeight := s.armHaltHeight(ctx, validators, fullNode, fullNodePeer)
	t.Logf("Phase 2: halt at height %d and roll back %d heights", phase2HaltHeight, rollbackDepth)
	s.waitForHalt(ctx, validators, fullNode, phase2HaltHeight)
	s.requireHalted(ctx, fullNode, phase2HaltHeight)

	target := phase2HaltHeight - 1 - rollbackDepth
	canonical := make(map[int64]string)
	for h := target + 1; h <= phase2HaltHeight; h++ {
		canonical[h] = s.block(ctx, fullNode, h).BlockID.Hash.String()
	}

	for _, v := range validators {
		s.Require().NotEmpty(s.snapshotsAbove(ctx, v, target), "no snapshot above height %d on %s", target, v.Name())
		out := s.recoveryRollback(ctx, v, target)
		s.Require().Contains(out, fmt.Sprintf("rollback: height %d -> %d, blocks to delete: %d", phase2HaltHeight-1, target, rollbackDepth))
		s.Require().Contains(out, fmt.Sprintf("warning: app.toml sets halt-height = %d", phase2HaltHeight))
		s.Require().Empty(s.snapshotsAbove(ctx, v, target), "snapshot above height %d left on %s", target, v.Name())

		// State is at target and no block above target+1 is left.
		out, err := s.rollback(ctx, v, "--height", strconv.FormatInt(target, 10), "--dry-run")
		s.Require().NoError(err, "rollback dry run failed on %s", v.Name())
		s.Require().Contains(out, fmt.Sprintf("rollback: height %d -> %d, blocks to delete: 0", target, target))
		s.Require().Contains(out, fmt.Sprintf("dry run: rollback to height %d is possible", target))
	}

	for _, v := range validators {
		s.restartWithHaltHeight(ctx, v, 0, fullNodePeer)
	}
	s.waitForHeight(ctx, allNodes, phase2HaltHeight+10)

	for h := target + 1; h <= phase2HaltHeight; h++ {
		s.requireBlockHash(ctx, validators, h, canonical[h])
	}
	s.requireBlockHash(ctx, allNodes, phase2HaltHeight+5, s.block(ctx, fullNode, phase2HaltHeight+5).BlockID.Hash.String())
	s.verifyAppHash(ctx, validators, fullNode, target+2)
	s.Require().NoError(s.CheckLiveness(ctx, celestia), "validator liveness check failed after phase 2")

	endHeight, err := fullNode.Height(ctx)
	s.Require().NoError(err)
	for h := startHeight; h <= endHeight; h++ {
		s.Require().Empty(s.block(ctx, fullNode, h).Block.Evidence.Evidence, "unexpected evidence at height %d", h)
	}
}

// armHaltHeight restarts the validators with a halt height above the full node's height and returns it.
// It stops every validator first so the chain cannot advance while the others are restarted.
func (s *CelestiaTestSuite) armHaltHeight(ctx context.Context, validators cosmos.ChainNodes, fullNode *cosmos.ChainNode, peer string) int64 {
	for _, v := range validators {
		s.Require().NoError(v.Stop(ctx), "failed to stop %s", v.Name())
	}
	height, err := fullNode.Height(ctx)
	s.Require().NoError(err)
	haltHeight := height + rollbackHaltOffset
	for _, v := range validators {
		s.restartWithHaltHeight(ctx, v, haltHeight, peer)
	}
	height, err = fullNode.Height(ctx)
	s.Require().NoError(err)
	s.Require().Less(height, haltHeight, "chain reached the halt height before all validators were restarted")
	return haltHeight
}

// restartWithHaltHeight sets halt-height in app.toml (0 disables it), adds peer to
// the persistent peers and restarts the node.
func (s *CelestiaTestSuite) restartWithHaltHeight(ctx context.Context, node *cosmos.ChainNode, height int64, peer string) {
	err := config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
		cfg.HaltHeight = uint64(height)
	})
	s.Require().NoError(err, "failed to set halt-height on %s", node.Name())
	err = config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
		if !strings.Contains(cfg.P2P.PersistentPeers, peer) {
			cfg.P2P.PersistentPeers = strings.TrimPrefix(cfg.P2P.PersistentPeers+","+peer, ",")
		}
	})
	s.Require().NoError(err, "failed to add a persistent peer on %s", node.Name())
	s.Require().NoError(node.Restart(ctx), "failed to restart %s", node.Name())
}

// waitForHalt waits until every validator container has exited and the full node is at height.
func (s *CelestiaTestSuite) waitForHalt(ctx context.Context, validators cosmos.ChainNodes, fullNode *cosmos.ChainNode, height int64) {
	err := wait.ForCondition(ctx, rollbackTimeout, 2*time.Second, func() (bool, error) {
		for _, v := range validators {
			res, err := s.client.ContainerInspect(ctx, v.ContainerLifecycle.ContainerName(), mobyclient.ContainerInspectOptions{})
			if err != nil {
				return false, err
			}
			if res.Container.State.Running {
				return false, nil
			}
		}
		fullNodeHeight, err := fullNode.Height(ctx)
		return err == nil && fullNodeHeight >= height, nil
	})
	s.Require().NoError(err, "validators did not halt at height %d", height)

	fullNodeHeight, err := fullNode.Height(ctx)
	s.Require().NoError(err)
	s.Require().Equal(height, fullNodeHeight, "chain did not stop at the halt height")
}

// requireHalted checks that the full node stays at or below height for 12 seconds.
func (s *CelestiaTestSuite) requireHalted(ctx context.Context, fullNode *cosmos.ChainNode, height int64) {
	for range 4 {
		time.Sleep(3 * time.Second)
		fullNodeHeight, err := fullNode.Height(ctx)
		s.Require().NoError(err)
		s.Require().LessOrEqual(fullNodeHeight, height, "chain advanced past the halt height")
	}
}

// waitForHeight waits until every node is above height.
func (s *CelestiaTestSuite) waitForHeight(ctx context.Context, nodes cosmos.ChainNodes, height int64) {
	for _, n := range nodes {
		err := s.WaitForSync(ctx, n.Client, rollbackTimeout, func(info coretypes.SyncInfo) bool {
			return info.LatestBlockHeight > height
		})
		s.Require().NoError(err, "%s did not pass height %d", n.Name(), height)
	}
}

// rollback runs celestia-appd rollback on a stopped node and returns stdout and stderr combined.
func (s *CelestiaTestSuite) rollback(ctx context.Context, node *cosmos.ChainNode, args ...string) (string, error) {
	cmd := append([]string{"celestia-appd", "rollback", "--home", node.HomeDir()}, args...)
	stdout, stderr, err := node.Exec(ctx, cmd, nil)
	return string(stdout) + string(stderr), err
}

// snapshotsAbove returns the heights of the local snapshots above height on a stopped node.
func (s *CelestiaTestSuite) snapshotsAbove(ctx context.Context, node *cosmos.ChainNode, height int64) []int64 {
	stdout, stderr, err := node.Exec(ctx, []string{"celestia-appd", "snapshots", "list", "--home", node.HomeDir()}, nil)
	s.Require().NoError(err, "snapshots list failed on %s: %s", node.Name(), stderr)
	var heights []int64
	for _, line := range strings.Split(string(stdout)+string(stderr), "\n") {
		var h, format, chunks int64
		if _, err := fmt.Sscanf(line, "height: %d format: %d chunks: %d", &h, &format, &chunks); err == nil && h > height {
			heights = append(heights, h)
		}
	}
	return heights
}

// recoveryRollback rolls a stopped node back to height with the recovery script and returns its output.
func (s *CelestiaTestSuite) recoveryRollback(ctx context.Context, node *cosmos.ChainNode, height int64) string {
	out, err := s.runRecoveryScript(ctx, node, "rollback", "--setup", "none", "--home", node.HomeDir(), "--height", strconv.FormatInt(height, 10))
	s.T().Logf("recovery.sh rollback on %s:\n%s", node.Name(), out)
	s.Require().NoError(err, "rollback failed on %s", node.Name())
	s.Require().Contains(out, fmt.Sprintf("dry run: rollback to height %d is possible", height))
	s.Require().Contains(out, fmt.Sprintf("Rolled back to height %d", height))
	return out
}

// verifyAppHash checks with the recovery script that each node has the full node's app hash at height.
func (s *CelestiaTestSuite) verifyAppHash(ctx context.Context, nodes cosmos.ChainNodes, fullNode *cosmos.ChainNode, height int64) {
	appHash := s.block(ctx, fullNode, height).Block.AppHash.String()
	for _, n := range nodes {
		out, err := s.runRecoveryScript(ctx, n, "verify", "--rpc", "http://"+n.HostName()+":26657",
			"--check-height", strconv.FormatInt(height, 10), "--expect-app-hash", appHash)
		s.Require().NoError(err, "app hash check failed on %s", n.Name())
		s.Require().Contains(out, fmt.Sprintf("app hash at height %d matches", height))
	}
}

// runRecoveryScript runs recovery.sh from the node home in a one-off container and returns stdout and stderr combined.
func (s *CelestiaTestSuite) runRecoveryScript(ctx context.Context, node *cosmos.ChainNode, args ...string) (string, error) {
	cmd := append([]string{"bash", node.HomeDir() + "/recovery.sh"}, args...)
	stdout, stderr, err := node.Exec(ctx, cmd, nil)
	return string(stdout) + string(stderr), err
}

func (s *CelestiaTestSuite) block(ctx context.Context, node *cosmos.ChainNode, height int64) *coretypes.ResultBlock {
	block, err := node.Client.Block(ctx, &height)
	s.Require().NoError(err, "failed to get block %d from %s", height, node.Name())
	return block
}

// requireBlockHash checks that every node has the given block hash at height.
func (s *CelestiaTestSuite) requireBlockHash(ctx context.Context, nodes cosmos.ChainNodes, height int64, want string) {
	for _, n := range nodes {
		s.Require().Equal(want, s.block(ctx, n, height).BlockID.Hash.String(), "block %d differs on %s", height, n.Name())
	}
}
