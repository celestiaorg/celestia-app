package docker_e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	signaltypes "github.com/celestiaorg/celestia-app/v5/x/signal/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
)

// TestCelestiaAppBinarySwapUpgrade tests a simple upgrade from one image tag
// to another by swapping the binary (usually used for minor version upgrade)
func (s *CelestiaTestSuite) TestCelestiaAppBinarySwapUpgrade() {
	if testing.Short() {
		s.T().Skip("skipping celestia-app minor upgrade test in short mode")
	}

	tt := []struct {
		Name           string
		BaseImageTag   string
		TargetImageTag string
	}{
		{
			Name:           "v4.0.2-rc2 to v4.0.10",
			BaseImageTag:   "v4.0.2-rc2",
			TargetImageTag: "v4.0.10",
		},
		{
			Name:           "v4.0.9-mocha to v4.0.10-mocha",
			BaseImageTag:   "v4.0.9-mocha",
			TargetImageTag: "v4.0.10-mocha",
		},
		{
			Name:           "v4.0.9-arabica to v4.0.10-arabica",
			BaseImageTag:   "v4.0.9-arabica",
			TargetImageTag: "v4.0.10-arabica",
		},
	}

	for _, tc := range tt {
		s.Run(tc.Name, func() {
			s.runBinarySwapUpgradeTest(tc.BaseImageTag, tc.TargetImageTag)
		})
	}
}

// TestCelestiaAppSignalDrivenUpgrade tests app version upgrade using the signaling mechanism.
func (s *CelestiaTestSuite) TestCelestiaAppSignalDrivenUpgrade() {
	if testing.Short() {
		s.T().Skip("skipping celestia-app major upgrade test in short mode")
	}

	tt := []struct {
		Name             string
		ImageTag         string
		TargetAppVersion uint64
	}{
		{
			Name:             "v2 to v3",
			ImageTag:         dockerchain.GetCelestiaTag(),
			TargetAppVersion: 3,
		},
		{
			Name:             "v3 to v4",
			ImageTag:         dockerchain.GetCelestiaTag(),
			TargetAppVersion: 4,
		},
	}

	for _, tc := range tt {
		s.Run(tc.Name, func() {
			s.runSignalDrivenUpgradeTest(tc.ImageTag, tc.TargetAppVersion)
		})
	}
}

// runBinarySwapUpgradeTest tests a binary swap (minor version) upgrade for celestia-app.
// It initializes a chain with the given base image tag, verifies bank send functionality,
// upgrades the chain to the target image tag, restarts it, and then verifies both the
// new binary version and continued bank send functionality.
func (s *CelestiaTestSuite) runBinarySwapUpgradeTest(BaseImageTag, TargetImageTag string) {
	var (
		ctx = context.Background()
		cfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(BaseImageTag)
	)

	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		if err := chain.Stop(ctx); err != nil {
			s.T().Logf("Error stopping chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err)

	// Sanity check: Test bank send before upgrade
	s.T().Log("Testing bank send functionality before upgrade")
	testBankSend(s.T(), chain, cfg)

	err = chain.Stop(ctx)
	s.Require().NoError(err)

	chain.UpgradeVersion(ctx, TargetImageTag)

	err = chain.Start(ctx)
	s.Require().NoError(err)

	// Sanity check: Test bank send after upgrade
	s.T().Log("Testing bank send functionality after upgrade")
	testBankSend(s.T(), chain, cfg)

	// Verify the binary version after upgrade
	validatorNode := chain.GetNodes()[0]

	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client for version check")

	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Contains(abciInfo.Response.GetVersion(), strings.TrimPrefix(TargetImageTag, "v"), "version mismatch")
}

// runSignalDrivenUpgradeTest performs an end-to-end test of a major (signaled) upgrade for celestia-app.
// It starts a chain at the given image tag with app version set to one less than the target,
// signals all validators for the upgrade, ensures the upgrade is scheduled and executed,
// and verifies the chain is running the new binary and app version after the upgrade.
// The test also checks bank send functionality and that the voting power threshold is met before proceeding.
func (s *CelestiaTestSuite) runSignalDrivenUpgradeTest(ImageTag string, TargetAppVersion uint64) {
	// Since the signaling mechanism was introduced in v2, we need to ensure that
	// the target app version is greater than 2.
	s.Require().Greater(TargetAppVersion, uint64(2), "target app version must be greater than 2")

	var (
		ctx = context.Background()
		cfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(ImageTag)
		kr  = cfg.Genesis.Keyring()
	)
	cfg.Genesis = cfg.Genesis.WithAppVersion(TargetAppVersion - 1)

	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		if err := chain.Stop(ctx); err != nil {
			s.T().Logf("Error stopping chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err)

	// Sanity check: Test bank send before upgrade
	s.T().Log("Testing bank send functionality before upgrade")
	testBankSend(s.T(), chain, cfg)

	validatorNode := chain.GetNodes()[0]
	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	currentAppVer := abciInfo.Response.GetAppVersion()
	s.T().Logf("Current app version before upgrade: %d", currentAppVer)

	records, err := kr.List()
	s.Require().NoError(err, "failed to list accounts")
	s.Require().Len(records, len(chain.GetNodes()), "number of accounts does not match number of nodes")

	// Signal for upgrade and get the upgrade height
	upgradeHeight := s.signalAndGetUpgradeHeight(ctx, chain, validatorNode, records, TargetAppVersion)

	// Get current height
	status, err := rpcClient.Status(ctx)
	s.Require().NoError(err, "failed to get node status")
	currentHeight := status.SyncInfo.LatestBlockHeight

	s.T().Logf("Current height: %d, Upgrade height: %d", currentHeight, upgradeHeight)

	// Wait until we reach the upgrade height
	blocksToWait := int(upgradeHeight-currentHeight) + 2 // Add buffer
	s.T().Logf("Waiting for %d blocks to reach upgrade height plus buffer", blocksToWait)
	s.Require().NoError(wait.ForBlocks(ctx, blocksToWait, chain))

	// Verify the app version has been upgraded
	abciInfo, err = rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")

	// Verify app version is upgraded
	s.Require().Equal(TargetAppVersion, abciInfo.Response.GetAppVersion(), "app_version mismatch")

	// Sanity check: Test bank send after upgrade
	s.T().Log("Testing bank send functionality after upgrade")
	testBankSend(s.T(), chain, cfg)
}

// signalAndGetUpgradeHeight signals for an upgrade to the specified app
// version and returns the scheduled upgrade height.
func (s *CelestiaTestSuite) signalAndGetUpgradeHeight(ctx context.Context, chain tastoratypes.Chain, validatorNode tastoratypes.ChainNode, records []*keyring.Record, targetAppVersion uint64) int64 {
	// Signal for the upgrade using builder
	for i, node := range chain.GetNodes() {
		s.T().Logf("Signaling for upgrade to version %d from validator %d", targetAppVersion, i)

		signalCmd := []string{"tx", "signal", "signal", fmt.Sprintf("%d", targetAppVersion), "--from", records[i].Name}
		cmdArgs, err := NewCommandBuilder(ctx, node, signalCmd).WithFees("200000utia").Build()
		s.Require().NoError(err)
		_, stderrBytes, err := node.Exec(ctx, cmdArgs, nil)
		s.Require().NoError(err, "failed to signal for upgrade: %s", string(stderrBytes))
	}

	s.Require().NoError(wait.ForBlocks(ctx, 1, chain))

	s.validateSignalTally(ctx, validatorNode, targetAppVersion)

	// Execute try-upgrade using builder
	tryUpgradeCmd := []string{"tx", "signal", "try-upgrade", "--from", records[0].Name}
	tryArgs, err := NewCommandBuilder(ctx, validatorNode, tryUpgradeCmd).Build()
	_, upgradeStderrBytes, err := validatorNode.Exec(ctx, tryArgs, nil)
	s.Require().NoError(err, "failed to execute try-upgrade: %s", string(upgradeStderrBytes))

	// Wait for one block so that the upgrade transaction is processed
	s.Require().NoError(wait.ForBlocks(ctx, 1, chain))

	// New approach: use gRPC Query client for typed response
	s.T().Log("Querying upgrade info via gRPC")
	client, cleanup, err := getSignalQueryClient(ctx, validatorNode)
	s.Require().NoError(err)
	defer cleanup()

	upgradeResp, err := client.
		GetUpgrade(ctx, &signaltypes.QueryGetUpgradeRequest{})
	s.Require().NoError(err, "failed to query upgrade info via gRPC")

	// Ensure an upgrade is indeed pending
	s.Require().NotNil(upgradeResp.Upgrade, "no upgrade pending after try-upgrade")

	upgradeHeight := upgradeResp.Upgrade.UpgradeHeight
	s.T().Logf("Upgrade info: app_version=%d height=%d", upgradeResp.Upgrade.AppVersion, upgradeHeight)

	return upgradeHeight
}

// validateSignalTally queries the signal tally for the given app version and verifies
// that the voting power meets or exceeds the threshold power.
func (s *CelestiaTestSuite) validateSignalTally(ctx context.Context, node tastoratypes.ChainNode, appVersion uint64) {
	s.T().Logf("Querying signal tally for app version %d", appVersion)

	client, cleanup, err := getSignalQueryClient(ctx, node)
	s.Require().NoError(err)
	defer cleanup()

	resp, err := client.VersionTally(ctx, &signaltypes.QueryVersionTallyRequest{Version: appVersion})
	s.Require().NoError(err, "failed to query tally")

	// Verify that voting power meets or exceeds threshold
	s.Require().True(resp.VotingPower >= resp.ThresholdPower, "voting power (%d) does not meet threshold (%d)", resp.VotingPower, resp.ThresholdPower)
}

// getSignalQueryClient returns a signaltypes.QueryClient for the provided node.
// If the node already exposes a live *grpc.ClientConn (docker ChainNode), that
// connection is reused and the returned cleanup is a no-op. Otherwise the
// helper dials the node’s gRPC endpoint (port 9090) and returns a cleanup
// function that closes the connection.
func getSignalQueryClient(ctx context.Context, node tastoratypes.ChainNode) (signaltypes.QueryClient, func(), error) {
	if dcNode, ok := node.(*tastoradockertypes.ChainNode); ok && dcNode.GrpcConn != nil {
		return signaltypes.NewQueryClient(dcNode.GrpcConn), func() {}, nil
	}
	host, err := node.GetInternalHostName(ctx)
	if err != nil {
		return nil, nil, err
	}
	endpoint := fmt.Sprintf("%s:9090", host)
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = conn.Close() }
	return signaltypes.NewQueryClient(conn), cleanup, nil
}
