package docker_e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	cometcfg "github.com/cometbft/cometbft/config"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	servercfg "github.com/cosmos/cosmos-sdk/server/config"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

const (
	// exportRestartDelay is how far ahead of the restart the new genesis time is set.
	exportRestartDelay = 10 * time.Second
	// restartHeightGap is how many heights the new chain leaves free above the
	// highest old height, so no new header or vote shares a height with an old one.
	restartHeightGap = 10
)

// exportRestart describes a restart of a chain from an export.
type exportRestart struct {
	// newChainID is the chain-id of the new chain; empty keeps the old one.
	newChainID string
	// archive is a node of the old chain that keeps its data and stays out of
	// the new chain, so relayers can read the old blocks and events.
	archive *cosmos.ChainNode

	// Filled in by exportAndRestart.

	// exportHeight is the last executed height of the old chain (E).
	exportHeight int64
	// signedHeight is the highest height in any validator's sign state (S).
	signedHeight int64
	// initialHeight is the first height of the new chain.
	initialHeight int64
	// exported is the export the new chain starts from.
	exported *genutiltypes.AppGenesis
}

// exportAndRestart stops the chain, exports the highest validator's state and
// restarts every validator as a new chain from that export.
//
// The new chain starts at max(S, E) + 1 + restartHeightGap, where S is the
// highest height in any validator's sign state and E the export height. Every
// old commit needs precommits of these validators, so no old block is above S.
// A real recovery must also read remote signers. The sign state is kept, so
// no validator signs an old height again: old votes cannot become double-sign
// evidence and relayers see no conflicting headers.
func (s *CelestiaTestSuite) exportAndRestart(ctx context.Context, chain *cosmos.Chain, r *exportRestart) {
	t := s.T()
	// The archive must hold every block a relayer still needs, so wait until it
	// has the validators' latest height before stopping consensus.
	var archiveMin int64
	if r.archive != nil {
		h, err := chain.Validators[0].Height(ctx)
		s.Require().NoError(err)
		archiveMin = h
		err = s.WaitForSync(ctx, r.archive.Client, time.Minute, func(info coretypes.SyncInfo) bool {
			return info.LatestBlockHeight >= archiveMin
		})
		s.Require().NoError(err, "archive did not reach height %d", archiveMin)
	}
	s.Require().NoError(chain.Stop(ctx), "failed to stop the chain")

	var source *cosmos.ChainNode
	for _, v := range chain.Validators {
		exported := s.export(ctx, v)
		t.Logf("%s exported height %d", v.Name(), exported.InitialHeight-1)
		if r.exported == nil || exported.InitialHeight > r.exported.InitialHeight {
			r.exported, source = exported, v
		}
		if h := s.signedHeight(ctx, v); h > r.signedHeight {
			r.signedHeight = h
		}
	}
	r.exportHeight = r.exported.InitialHeight - 1
	r.initialHeight = max(r.signedHeight, r.exportHeight) + 1 + restartHeightGap

	// An export below the last height must work too: it is the way back to a
	// height before the bad state.
	previous := s.export(ctx, source, "--height", strconv.FormatInt(r.exportHeight-1, 10))
	s.Require().Equal(r.exportHeight, previous.InitialHeight, "export --height %d returned another height", r.exportHeight-1)
	previousBz, err := json.Marshal(previous)
	s.Require().NoError(err)
	s.validateGenesis(ctx, source, previousBz)
	t.Logf("export height %d, signed height %d, new chain starts at %d", r.exportHeight, r.signedHeight, r.initialHeight)

	genesis := *r.exported
	genesis.InitialHeight = r.initialHeight
	genesis.GenesisTime = time.Now().Add(exportRestartDelay).UTC()
	if r.newChainID != "" {
		genesis.ChainID = r.newChainID
	}
	genesisBz, err := json.Marshal(&genesis)
	s.Require().NoError(err)
	s.validateGenesis(ctx, source, genesisBz)

	for _, v := range chain.Validators {
		s.Require().NoError(v.WriteFile(ctx, "config/genesis.json", genesisBz), "failed to write genesis on %s", v.Name())
		// Keep the sign state, which forbids signing old heights again, and the
		// tx index, which holds the events of packets sent before the halt.
		wipe := fmt.Sprintf("find %s/data -mindepth 1 -maxdepth 1 ! -name priv_validator_state.json ! -name tx_index.db -exec rm -rf {} +", v.HomeDir())
		_, stderr, err := v.Exec(ctx, []string{"sh", "-c", wipe}, nil)
		s.Require().NoError(err, "failed to wipe data on %s: %s", v.Name(), stderr)
	}
	if r.archive != nil {
		s.isolateArchive(ctx, r.archive)
	}
	if r.newChainID != "" {
		// Tastora signs with the chain-id of the node it broadcasts through.
		chain.Config.ChainID = r.newChainID
		for _, n := range chain.Nodes() {
			n.ChainID = r.newChainID
		}
	}

	s.Require().NoError(chain.Restart(ctx), "failed to restart the chain")

	node := chain.Validators[0]
	err = s.WaitForSync(ctx, node.Client, 90*time.Second, func(info coretypes.SyncInfo) bool {
		return info.LatestBlockHeight > r.initialHeight+2
	})
	s.Require().NoError(err, "new chain did not produce blocks after height %d", r.initialHeight)

	status, err := node.Client.Status(ctx)
	s.Require().NoError(err)
	s.Require().Equal(r.initialHeight, status.SyncInfo.EarliestBlockHeight, "new chain does not start at the initial height")
	s.Require().Equal(genesis.ChainID, status.NodeInfo.Network, "new chain has an unexpected chain-id")
	info, err := node.Client.ABCIInfo(ctx)
	s.Require().NoError(err)
	s.Require().Equal(appconsts.Version, info.Response.GetAppVersion(), "unexpected app version on the new chain")
	for h := r.initialHeight; h <= r.initialHeight+2; h++ {
		s.Require().Empty(s.block(ctx, node, h).Block.Evidence.Evidence, "unexpected evidence at height %d", h)
	}
	if r.archive != nil {
		archive, err := r.archive.Client.Status(ctx)
		s.Require().NoError(err, "archive does not serve RPC after the restart")
		s.Require().GreaterOrEqual(archive.SyncInfo.LatestBlockHeight, archiveMin, "archive lost blocks across the restart")
		s.Require().Less(archive.SyncInfo.LatestBlockHeight, r.initialHeight, "archive joined the new chain")
	}
}

// export runs celestia-appd export on a stopped node and returns the parsed genesis.
func (s *CelestiaTestSuite) export(ctx context.Context, node *cosmos.ChainNode, args ...string) *genutiltypes.AppGenesis {
	cmd := append([]string{"celestia-appd", "export", "--home", node.HomeDir(), "--output-document", node.HomeDir() + "/export.json"}, args...)
	_, stderr, err := node.Exec(ctx, cmd, nil)
	s.Require().NoError(err, "export failed on %s: %s", node.Name(), stderr)

	bz, err := node.ReadFile(ctx, "export.json")
	s.Require().NoError(err, "failed to read the export on %s", node.Name())
	genesis, err := genutiltypes.AppGenesisFromReader(bytes.NewReader(bz))
	s.Require().NoError(err, "failed to parse the export on %s", node.Name())
	return genesis
}

// validateGenesis runs celestia-appd genesis validate on genesisBz in a stopped node.
func (s *CelestiaTestSuite) validateGenesis(ctx context.Context, node *cosmos.ChainNode, genesisBz []byte) {
	s.Require().NoError(node.WriteFile(ctx, "validate-genesis.json", genesisBz))
	cmd := []string{"celestia-appd", "genesis", "validate", node.HomeDir() + "/validate-genesis.json", "--home", node.HomeDir()}
	stdout, stderr, err := node.Exec(ctx, cmd, nil)
	s.Require().NoError(err, "genesis validate failed: %s%s", stdout, stderr)
}

// signedHeight returns the height in a stopped validator's sign state.
func (s *CelestiaTestSuite) signedHeight(ctx context.Context, node *cosmos.ChainNode) int64 {
	bz, err := node.ReadFile(ctx, "data/priv_validator_state.json")
	s.Require().NoError(err, "failed to read the sign state on %s", node.Name())
	var state struct {
		Height string `json:"height"`
	}
	s.Require().NoError(json.Unmarshal(bz, &state), "failed to parse the sign state on %s", node.Name())
	height, err := strconv.ParseInt(state.Height, 10, 64)
	s.Require().NoError(err)
	return height
}

// isolateArchive keeps a stopped old-chain node out of the new chain: it
// dials no peer and accepts none, and only serves its old blocks over RPC.
func (s *CelestiaTestSuite) isolateArchive(ctx context.Context, node *cosmos.ChainNode) {
	err := config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
		cfg.P2P.PersistentPeers = ""
		cfg.P2P.Seeds = ""
		cfg.P2P.PexReactor = false
		cfg.P2P.MaxNumInboundPeers = 0
		cfg.P2P.MaxNumOutboundPeers = 0
	})
	s.Require().NoError(err, "failed to isolate the archive %s", node.Name())
}

// addArchiveNode adds a full node to the chain that later serves the old
// blocks: exportAndRestart leaves its data in place and isolates it.
func (s *CelestiaTestSuite) addArchiveNode(ctx context.Context, chain *cosmos.Chain) *cosmos.ChainNode {
	err := chain.AddNode(ctx, cosmos.NewChainNodeConfigBuilder().
		WithNodeType(tastoratypes.NodeTypeConsensusFull).
		WithPostInit(func(ctx context.Context, node *cosmos.ChainNode) error {
			// Tastora enables REST, which requires the Cosmos SDK gRPC server.
			return config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
				cfg.GRPC.Enable = true
			})
		}).
		Build())
	s.Require().NoError(err, "failed to add the archive node")
	archive := chain.FullNodes[len(chain.FullNodes)-1]
	err = s.WaitForSync(ctx, archive.Client, 2*time.Minute, func(info coretypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight > 0
	})
	s.Require().NoError(err, "archive node failed to sync")
	return archive
}
