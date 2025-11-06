package docker_e2e

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/test/util/genesis"
	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker/cosmos"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
)

// apiResponses holds the API responses from a single node for compatibility testing
type apiResponses struct {
	nodeIndex int
	version   string
	status    *rpctypes.ResultStatus
	genesis   *rpctypes.ResultGenesis
	abciInfo  *rpctypes.ResultABCIInfo
}

// TestMinorVersionCompatibility tests that nodes running different minor versions
// of the same major app version can coexist without causing network halts.
// This test ensures backward compatibility within the same major version.
func (s *CelestiaTestSuite) TestMinorVersionCompatibility() {
	if testing.Short() {
		s.T().Skip("skipping minor version compatibility test in short mode")
	}

	// Test different combinations of version tags
	// Each tag will create its own validator node
	testCases := []struct {
		name string
		tags []string
	}{
		{
			name: "v6 minor versions",
			tags: []string{"v6.0.0-arabica", "v6.0.1-arabica", "v6.0.2-arabica", "v6.0.3-arabica", "v6.0.4-arabica", "v6.0.5-arabica"},
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// If the test case version matches the main branch version, add the main branch tag to the test case
			tcAppVersion := s.extractMajorVersionFromTag(tc.tags[0])
			if tcAppVersion == appconsts.Version {
				mainBranchTag, err := dockerchain.GetCelestiaTagStrict()
				s.Require().NoError(err)
				tc.tags = append(tc.tags, mainBranchTag)
			}
			s.runMinorVersionCompatibilityTest(ctx, tc.tags)
		})
	}
}

// runMinorVersionCompatibilityTest executes the actual compatibility test
// for the given set of version tags
func (s *CelestiaTestSuite) runMinorVersionCompatibilityTest(ctx context.Context, versionTags []string) {
	t := s.T()

	if len(versionTags) < 1 {
		t.Skip("Need at least 1 version tag to test")
	}

	t.Logf("Testing compatibility between versions: %v", versionTags)
	chain, cfg := s.buildMixedVersionChain(ctx, versionTags)

	t.Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	s.Require().NoError(chain.Start(ctx), "failed to start mixed version chain")
	s.verifyAPICompatibilityAcrossVersions(ctx, chain, versionTags)

	testBankSend(s.T(), chain, cfg)
	testPFBSubmission(s.T(), chain, cfg)

	s.Require().NoError(s.CheckLiveness(ctx, chain), "liveness check failed - network may have halted")

	t.Logf("Minor version compatibility test passed for versions: %v", versionTags)
}

// buildMixedVersionChain creates a chain with nodes running different version tags
func (s *CelestiaTestSuite) buildMixedVersionChain(ctx context.Context, versionTags []string) (*tastoradockertypes.Chain, *dockerchain.Config) {
	t := s.T()

	// Use the first version tag as the base configuration
	baseTag := versionTags[0]
	baseCfg := dockerchain.DefaultConfig(s.client, s.network).WithTag(baseTag)

	validators := make([]genesis.Validator, len(versionTags))
	for i := range len(versionTags) {
		validators[i] = genesis.NewDefaultValidator(fmt.Sprintf("validator%d", i))
	}
	baseCfg.Genesis = genesis.NewDefaultGenesis().WithChainID(appconsts.TestChainID).WithValidators(validators...)

	// Get individual node config builders to assign different versions
	nodeBuilders, err := dockerchain.NodeConfigBuilders(baseCfg)
	s.Require().NoError(err, "failed to get node config builders")

	nodeConfigs := make([]tastoradockertypes.ChainNodeConfig, len(nodeBuilders))
	for i, nb := range nodeBuilders {
		nodeConfigs[i] = nb.WithImage(tastoracontainertypes.NewImage(baseCfg.Image, versionTags[i], "10001:10001")).Build()
	}

	// The Cosmos SDK requires consensus.params.version.app to be set in genesis for proper chain startup.
	// This version must match what the binary reports to avoid genesis validation errors.
	// We extract the major version from the Docker tag (e.g., "v5.0.5" -> 5) and use it consistently.
	appVersion := s.extractMajorVersionFromTag(versionTags[0])
	baseCfg.Genesis = baseCfg.Genesis.WithAppVersion(appVersion)

	chain, err := dockerchain.NewCelestiaChainBuilder(t, baseCfg).WithNodes(nodeConfigs...).Build(ctx)
	s.Require().NoError(err, "failed to build mixed version chain")

	t.Logf("Successfully built mixed version chain with %d nodes using versions: %v", len(nodeConfigs), versionTags)
	return chain, baseCfg
}

// verifyAPICompatibilityAcrossVersions tests RPC API compatibility across different minor versions
// This ensures that API responses are structurally consistent and clients built for one minor version
// can successfully communicate with nodes running other minor versions of the same major version.
func (s *CelestiaTestSuite) verifyAPICompatibilityAcrossVersions(ctx context.Context, chain *tastoradockertypes.Chain, versionTags []string) {
	t := s.T()

	nodes := chain.GetNodes()
	s.Require().NotEmpty(nodes, "no nodes available for testing")

	// Collect API responses from all nodes for comparison
	var allResponses []apiResponses

	for i, node := range nodes {
		binaryVersion := versionTags[i]
		t.Logf("Testing APIs on node %d (version %s)", i, binaryVersion)

		rpcClient, err := node.GetRPCClient()
		s.Require().NoError(err, "failed to get RPC client for node %d", i)

		resp := apiResponses{
			nodeIndex: i,
			version:   binaryVersion,
		}

		resp.status, err = rpcClient.Status(ctx)
		s.Require().NoError(err, "Status API failed on node %d (version %s)", i, binaryVersion)
		s.Require().False(resp.status.SyncInfo.CatchingUp, "node %d should not be catching up", i)

		resp.genesis, err = rpcClient.Genesis(ctx)
		s.Require().NoError(err, "Genesis API failed on node %d (version %s)", i, binaryVersion)

		resp.abciInfo, err = rpcClient.ABCIInfo(ctx)
		s.Require().NoError(err, "ABCI Info API failed on node %d (version %s)", i, binaryVersion)

		allResponses = append(allResponses, resp)
		t.Logf("Node %d (%s): App version %d, ABCI version %s", i, binaryVersion, resp.abciInfo.Response.GetAppVersion(), resp.abciInfo.Response.GetVersion())
	}

	s.verifyResponseCompatibility(allResponses)
	t.Log("API compatibility verification passed across all versions")
}

// verifyResponseCompatibility ensures API responses are compatible across different minor versions
// This is the core of API breakage detection - ensuring response structures, field types,
// and critical values remain consistent across minor version updates.
func (s *CelestiaTestSuite) verifyResponseCompatibility(responses []apiResponses) {
	s.Require().GreaterOrEqual(len(responses), 2, "need at least 2 nodes for compatibility comparison")

	s.verifyGenesisCompatibility(responses)
	s.verifyStatusCompatibility(responses)
	s.verifyABCIInfoCompatibility(responses)

	s.T().Log("All API responses are compatible across versions")
}

// verifyGenesisCompatibility ensures Genesis responses are identical across versions
// Any difference in genesis indicates a critical incompatibility
func (s *CelestiaTestSuite) verifyGenesisCompatibility(responses []apiResponses) {
	baseGenesis := responses[0].genesis
	for i := range len(responses) {
		resp := responses[i]

		s.Require().Equal(baseGenesis.Genesis.ChainID, resp.genesis.Genesis.ChainID, "Genesis chain_id differs between %s and %s - this indicates incompatible networks", responses[0].version, resp.version)
		s.Require().Equal(baseGenesis.Genesis.GenesisTime, resp.genesis.Genesis.GenesisTime, "Genesis genesis_time differs between %s and %s - this indicates different networks", responses[0].version, resp.version)
		s.Require().Equal(baseGenesis.Genesis.AppHash, resp.genesis.Genesis.AppHash, "Genesis app_hash differs between %s and %s - this indicates incompatible genesis", responses[0].version, resp.version)

		s.T().Logf("Genesis compatibility verified: %s <-> %s", responses[0].version, resp.version)
	}
}

// verifyStatusCompatibility ensures Status responses have compatible structure and content
func (s *CelestiaTestSuite) verifyStatusCompatibility(responses []apiResponses) {

	baseStatus := responses[0].status
	for i := range len(responses) {
		resp := responses[i]

		s.Require().Equal(baseStatus.NodeInfo.Network, resp.status.NodeInfo.Network, "Status network differs between %s and %s - nodes on different chains", responses[0].version, resp.version)
		s.Require().Equal(baseStatus.NodeInfo.ProtocolVersion, resp.status.NodeInfo.ProtocolVersion, "Protocol version differs between %s and %s - potential consensus incompatibility", responses[0].version, resp.version)
		s.Require().False(resp.status.SyncInfo.CatchingUp, "Node %d (%s) is catching up - indicates sync issues", resp.nodeIndex, resp.version)

		// Height reasonableness: Heights should be within reasonable range (+/- 10 blocks)
		baseHeight := baseStatus.SyncInfo.LatestBlockHeight
		respHeight := resp.status.SyncInfo.LatestBlockHeight
		s.Require().InDelta(float64(baseHeight), float64(respHeight), 10.0, "Heights too different between %s (height %d) and %s (height %d) - indicates sync problems", responses[0].version, baseHeight, resp.version, respHeight)

		s.T().Logf("Status compatibility verified: %s <-> %s", responses[0].version, resp.version)
	}
}

// verifyABCIInfoCompatibility ensures ABCI Info responses are compatible across versions
func (s *CelestiaTestSuite) verifyABCIInfoCompatibility(responses []apiResponses) {
	baseABCI := responses[0].abciInfo
	for i := range len(responses) {
		resp := responses[i]

		// Critical: App version must be same across all nodes for compatibility
		s.Require().Equal(baseABCI.Response.GetAppVersion(), resp.abciInfo.Response.GetAppVersion(), "App versions differ between %s (app version %d) and %s (app version %d) - major version incompatibility", responses[0].version, baseABCI.Response.GetAppVersion(), resp.version, resp.abciInfo.Response.GetAppVersion())

		// Check only on semantic version like "v6.0.5-arabica" as the ci fails to pass the full commit hash
		if strings.HasPrefix(resp.version, "v") {
			expectedMinorVersion := strings.TrimPrefix(resp.version, "v")
			s.Require().Contains(resp.abciInfo.Response.GetVersion(), expectedMinorVersion, "Node %d reports ABCI version '%s', should contain '%s'", resp.nodeIndex, resp.abciInfo.Response.GetVersion(), expectedMinorVersion)
		}

		s.T().Logf("ABCI Info compatibility verified: %s <-> %s (both app version %d)", responses[0].version, resp.version, resp.abciInfo.Response.GetAppVersion())
	}
}

// extractMajorVersionFromTag extracts the major version number from a version tag
// Examples: "v5.0.5" -> 5, "v6.1.0" -> 6, "v10.2.1" -> 10, "v12.0.0" -> 12
// Fails the test if the tag format is invalid
func (s *CelestiaTestSuite) extractMajorVersionFromTag(tag string) uint64 {
	s.Require().NotEmpty(tag, "Version tag cannot be empty")

	// Regex to match version format vX.Y.Z and capture major version
	versionRegex := regexp.MustCompile(`^v(\d+)\.\d+\.\d+`)
	matches := versionRegex.FindStringSubmatch(tag)
	s.Require().Len(matches, 2, "Invalid version tag format: %s. Expected format: vX.Y.Z (e.g., v5.0.5, v10.1.0)", tag)

	// Convert captured major version to number
	majorVersion, err := strconv.ParseUint(matches[1], 10, 64)
	s.Require().NoError(err, "Invalid major version in tag: %s. Major version '%s' must be a valid number", tag, matches[1])

	return majorVersion
}
