package docker_e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"testing"
	"time"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"celestiaorg/celestia-app/test/docker-e2e/internal"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	mobyclient "github.com/moby/moby/client"
)

const (
	// knobsBaseAppVersion is the first app version with signalled upgrades.
	knobsBaseAppVersion = 2
	// deferredCatchUpLag is how far behind the wall clock a block must be for
	// deferred persistence "sync" to defer its writes. The node that syncs from
	// genesis starts after the chain is older than this, so the early blocks
	// run deferred and the later ones do not.
	deferredCatchUpLag = 10 * time.Minute
	knobsSyncTimeout   = 30 * time.Minute
)

// knobsAppTOML returns the [performance] keys of app.toml with every sync
// speedup on or off. blocksync-events-off stays false on every node: it is
// meant to change /block_results and is not part of the default set.
// Older binaries ignore the keys they do not know.
func knobsAppTOML(on bool) map[string]string {
	b := strconv.FormatBool(on)
	mode := `"off"`
	if on {
		mode = `"sync"`
	}
	return map[string]string{
		"parallel-commit":      b,
		"async-pruning":        b,
		"deferred-persistence": mode,
		"execution-caches":     b,
		"signature-preverify":  b,
		"delivertx-batch":      b,
		"blocksync-events-off": "false",
	}
}

// withKnobs returns post-init functions that apply the default node config
// and then turn every sync speedup on or off. appExtra holds extra app.toml
// keys by table ("" is the top level).
func withKnobs(on bool, appExtra map[string]map[string]string) []func(context.Context, *cosmos.ChainNode) error {
	b := strconv.FormatBool(on)
	return append(dockerchain.DefaultPostInit(), func(ctx context.Context, node *cosmos.ChainNode) error {
		if err := editTOML(ctx, node, "config/app.toml", "performance", knobsAppTOML(on)); err != nil {
			return err
		}
		for _, section := range slices.Sorted(maps.Keys(appExtra)) {
			if err := editTOML(ctx, node, "config/app.toml", section, appExtra[section]); err != nil {
				return err
			}
		}
		if err := editTOML(ctx, node, "config/config.toml", "", map[string]string{"parallel_commit_verify": b}); err != nil {
			return err
		}
		return editTOML(ctx, node, "config/config.toml", "blocksync", map[string]string{"pipeline": b})
	})
}

// editTOML sets keys of one table in a node's config file without decoding it
// into this module's config structs, which would drop the keys they lack.
func editTOML(ctx context.Context, node *cosmos.ChainNode, path, section string, values map[string]string) error {
	bz, err := node.ReadFile(ctx, path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return node.WriteFile(ctx, path, internal.SetTOMLKeys(bz, section, values))
}

// TestSyncKnobsDeterminism checks that the sync speedups never change what a
// node computes. Validators run with every speedup on, a full node with every
// speedup off, and the chain upgrades through every app version from v2 to
// the latest. A third node then block syncs the whole history from genesis
// with every speedup on, pruning "everything" (so deferred persistence is
// active for blocks older than deferredCatchUpLag) and a SIGKILL halfway. All
// nodes must agree on the block and app hash of every height and return
// identical /block_results (tx results, gas, events, validator and consensus
// param updates).
//
// Set CELESTIA_IMAGE and CELESTIA_TAG to a multiplexer image that contains the
// speedups. On an image without them the knob keys are ignored and the test
// checks ordinary multi-version determinism.
func (s *CelestiaTestSuite) TestSyncKnobsDeterminism() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping sync knobs determinism test in short mode")
	}
	ctx := context.Background()

	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err)
	cfg := dockerchain.DefaultConfig(s.client, s.network).WithTag(tag)
	cfg.Genesis = cfg.Genesis.WithAppVersion(knobsBaseAppVersion)

	var chain *cosmos.Chain
	err = retryOnPortCollision(ctx, 3, 2*time.Second, func() error {
		built, err := dockerchain.NewCelestiaChainBuilder(t, cfg).
			WithPostInit(withKnobs(true, nil)...).
			Build(ctx)
		if err != nil {
			return err
		}
		if err := built.Start(ctx); err != nil {
			if removeErr := built.Remove(ctx); removeErr != nil {
				t.Logf("Error removing chain after failed start: %v", removeErr)
			}
			return err
		}
		chain = built
		return nil
	})
	s.Require().NoError(err)
	t.Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			t.Logf("Error removing chain: %v", err)
		}
	})

	offNode := s.addKnobsNode(ctx, chain, withKnobs(false, nil))
	s.startConsistencyMonitor(ctx, chain.Nodes())

	testBankSend(t, chain, cfg)
	testPFBSubmission(t, chain, cfg)
	for v := uint64(knobsBaseAppVersion + 1); v <= appconsts.Version; v++ {
		t.Logf("Upgrading to app version %d", v)
		s.UpgradeChain(ctx, chain, cfg, v)
		testBankSend(t, chain, cfg)
		testPFBSubmission(t, chain, cfg)
	}

	validator := chain.Nodes()[0]
	targetHeight, err := validator.Height(ctx)
	s.Require().NoError(err)
	s.waitForDeferredLag(ctx, validator)

	// Pruning "everything" rejects state sync snapshots, so turn them off too.
	syncNode := s.addKnobsNode(ctx, chain, withKnobs(true, map[string]map[string]string{
		"":           {"pruning": `"everything"`, "min-retain-blocks": "0"},
		"state-sync": {"snapshot-interval": "0"},
	}))
	syncClient := s.rpcClient(syncNode)

	// Crash the node halfway through the sync and restart it on the same data.
	crashHeight := targetHeight / 2
	err = s.WaitForSync(ctx, syncClient, knobsSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return info.LatestBlockHeight >= crashHeight
	})
	s.Require().NoError(err, "sync node did not reach height %d", crashHeight)
	_, err = s.client.ContainerKill(ctx, syncNode.Name(), mobyclient.ContainerKillOptions{Signal: "SIGKILL"})
	s.Require().NoError(err, "failed to kill sync node")
	s.Require().NoError(syncNode.Restart(ctx), "failed to restart sync node after SIGKILL")
	syncClient = s.rpcClient(syncNode)

	err = s.WaitForSync(ctx, syncClient, knobsSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight >= targetHeight
	})
	s.Require().NoError(err, "sync node did not catch up to height %d", targetHeight)

	nodes := cosmos.ChainNodes{validator, offNode, syncNode}
	s.Require().NoError(internal.CheckRange(ctx, rpcNodes(nodes), 1, targetHeight))
	s.requireSameBlockResults(ctx, nodes, 1, targetHeight)
	t.Logf("Speedups on, off and a genesis block sync agreed on heights 1 to %d", targetHeight)
}

// addKnobsNode adds a full node with the given post-init functions and
// returns it.
func (s *CelestiaTestSuite) addKnobsNode(ctx context.Context, chain *cosmos.Chain, postInit []func(context.Context, *cosmos.ChainNode) error) *cosmos.ChainNode {
	err := chain.AddNode(ctx, cosmos.NewChainNodeConfigBuilder().
		WithNodeType(tastoratypes.NodeTypeConsensusFull).
		WithPostInit(postInit...).
		Build())
	s.Require().NoError(err, "failed to add full node")
	nodes := chain.Nodes()
	return nodes[len(nodes)-1]
}

func (s *CelestiaTestSuite) rpcClient(node *cosmos.ChainNode) rpcclient.Client {
	client, err := node.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client of %s", node.Name())
	return client
}

// waitForDeferredLag waits until block 1 is more than deferredCatchUpLag
// older than the wall clock, so a node syncing from genesis defers writes.
func (s *CelestiaTestSuite) waitForDeferredLag(ctx context.Context, node *cosmos.ChainNode) {
	height := int64(1)
	block, err := s.rpcClient(node).Block(ctx, &height)
	s.Require().NoError(err, "failed to fetch block 1")
	wait := time.Until(block.Block.Time.Add(deferredCatchUpLag + time.Minute))
	if wait <= 0 {
		return
	}
	s.T().Logf("Waiting %s until early blocks are old enough for deferred persistence", wait.Round(time.Second))
	select {
	case <-time.After(wait):
	case <-ctx.Done():
		s.Require().NoError(ctx.Err())
	}
}

// requireSameBlockResults fails on the first height where the nodes return
// different /block_results.
func (s *CelestiaTestSuite) requireSameBlockResults(ctx context.Context, nodes cosmos.ChainNodes, from, to int64) {
	clients := make([]rpcclient.Client, len(nodes))
	for i, n := range nodes {
		clients[i] = s.rpcClient(n)
	}
	for h := from; h <= to; h++ {
		var want []byte
		for i, c := range clients {
			res, err := c.BlockResults(ctx, &h)
			s.Require().NoError(err, "block_results of %s at height %d", nodes[i].Name(), h)
			got, err := json.Marshal(res)
			s.Require().NoError(err)
			if i == 0 {
				want = got
				continue
			}
			s.Require().True(bytes.Equal(want, got),
				"block_results differ at height %d\n%s: %s\n%s: %s",
				h, nodes[0].Name(), truncate(want), nodes[i].Name(), truncate(got))
		}
	}
}

func truncate(bz []byte) string {
	const limit = 4000
	if len(bz) > limit {
		return string(bz[:limit]) + "..."
	}
	return string(bz)
}
