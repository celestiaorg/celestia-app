package docker_e2e

import (
	"context"
	"fmt"
	"testing"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	signaltypes "github.com/celestiaorg/celestia-app/v6/x/signal/types"

	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TestCelestiaAppUpgrade tests app version upgrade using the signaling mechanism.
func (s *CelestiaTestSuite) TestCelestiaAppUpgrade() {
	if testing.Short() {
		s.T().Skip("skipping celestia-app major upgrade test in short mode")
	}

	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err)

	tt := []struct {
		baseAppVersion   uint64
		targetAppVersion uint64
	}{
		{
			baseAppVersion:   5,
			targetAppVersion: 6,
		},
	}

	for _, tc := range tt {
		s.Run(fmt.Sprintf("upgrade from v%d to v%d", tc.baseAppVersion, tc.targetAppVersion), func() {
			s.runUpgradeTest(tag, tc.baseAppVersion, tc.targetAppVersion)
		})
	}
}

// TestAllUpgrades tests all app version upgrades using the signaling mechanism.
// This test runs all upgrade paths.
func (s *CelestiaTestSuite) TestAllUpgrades() {
	if testing.Short() {
		s.T().Skip("skipping celestia-app TestAllUpgrades in short mode")
	}

	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err)

	// All upgrade paths for comprehensive testing
	tt := []struct {
		baseAppVersion   uint64
		targetAppVersion uint64
	}{
		{
			baseAppVersion:   2,
			targetAppVersion: 3,
		},
		{
			baseAppVersion:   3,
			targetAppVersion: 4,
		},
		{
			baseAppVersion:   4,
			targetAppVersion: 5,
		},
		{
			baseAppVersion:   5,
			targetAppVersion: 6,
		},
	}

	for _, tc := range tt {
		s.Run(fmt.Sprintf("upgrade from v%d to v%d", tc.baseAppVersion, tc.targetAppVersion), func() {
			s.runUpgradeTest(tag, tc.baseAppVersion, tc.targetAppVersion)
		})
	}
}

// runUpgradeTest starts a chain at the specified baseAppVersion, signals all validators to upgrade,
// waits for the upgrade to occur, then verifies the node is running the targetAppVersion and that
// bank send transactions succeed before and after the upgrade.
func (s *CelestiaTestSuite) runUpgradeTest(ImageTag string, baseAppVersion, targetAppVersion uint64) {
	// Signaling exists from v2 onwards, so target version must be >2.
	s.Require().Greater(targetAppVersion, uint64(2))

	var (
		ctx = context.Background()
		cfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(ImageTag)
		kr  = cfg.Genesis.Keyring()
	)
	cfg.Genesis = cfg.Genesis.WithAppVersion(baseAppVersion)

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

	s.T().Log("Testing PFB submission functionality before upgrade")
	testPFBSubmission(s.T(), chain, cfg)

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
	upgradeHeight := s.signalAndGetUpgradeHeight(ctx, chain, validatorNode, cfg, records, targetAppVersion)

	// Record start height - we'll use it later for health assertions
	status, err := rpcClient.Status(ctx)
	s.Require().NoError(err, "failed to get node status")
	startHeight := status.SyncInfo.LatestBlockHeight

	s.T().Logf("Start height: %d, Upgrade height: %d", startHeight, upgradeHeight)

	// Wait until we reach the upgrade height
	blocksToWait := int(upgradeHeight-startHeight) + 2 // Add buffer
	s.T().Logf("Waiting for %d blocks to reach upgrade height plus buffer", blocksToWait)
	s.Require().NoError(wait.ForBlocks(ctx, blocksToWait, chain))

	// Verify the app version has been upgraded
	abciInfo, err = rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")

	// Verify app version is upgraded
	s.Require().Equal(targetAppVersion, abciInfo.Response.GetAppVersion(), "app_version mismatch")

	// Sanity check: Test bank send after upgrade
	s.T().Log("Testing bank send functionality after upgrade")
	testBankSend(s.T(), chain, cfg)

	s.T().Log("Testing PFB submission functionality after upgrade")
	testPFBSubmission(s.T(), chain, cfg)

	s.T().Logf("Checking validator liveness from height %d with minimum %d blocks per validator", startHeight, defaultBlocksPerValidator)
	s.Require().NoError(
		s.CheckLiveness(ctx, chain),
		"validator liveness check failed",
	)
}

// signalAndGetUpgradeHeight signals for an upgrade to the specified app
// version and returns the scheduled upgrade height.
func (s *CelestiaTestSuite) signalAndGetUpgradeHeight(
	ctx context.Context,
	chain tastoratypes.Chain,
	validatorNode tastoratypes.ChainNode,
	cfg *dockerchain.Config,
	records []*keyring.Record,
	targetAppVersion uint64,
) int64 {
	// create a TxClient connected to the first validator gRPC endpoint
	cn, ok := validatorNode.(*tastoradockertypes.ChainNode)
	s.Require().True(ok, "validator node is not a docker chain node")

	txClient, err := dockerchain.SetupTxClient(ctx, cn, cfg)
	s.Require().NoError(err, "failed to setup TxClient for signaling")

	var (
		gasLimit = uint64(200_000)
		fee      = uint64(200_000)
	)

	// Signal for the upgrade
	for i, rec := range records {
		s.T().Logf("Signaling for upgrade to version %d from validator %d", targetAppVersion, i)

		addr, err := rec.GetAddress()
		s.Require().NoError(err)
		valAddr := sdk.ValAddress(addr).String()
		msg := signaltypes.NewMsgSignalVersion(valAddr, targetAppVersion)

		resp, err := txClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(gasLimit), user.SetFee(fee))
		s.Require().NoError(err, "failed to broadcast signal tx")
		s.Require().Equal(uint32(0), resp.Code, "signal tx failed with code %d", resp.Code)
	}

	// wait a block so the signals are included
	s.Require().NoError(wait.ForBlocks(ctx, 1, chain))

	s.validateSignalTally(ctx, validatorNode, targetAppVersion)

	msgTry := signaltypes.NewMsgTryUpgrade(txClient.DefaultAddress())
	resp, err := txClient.SubmitTx(ctx, []sdk.Msg{msgTry}, user.SetGasLimit(gasLimit), user.SetFee(fee))
	s.Require().NoError(err, "failed to broadcast try-upgrade tx")
	s.Require().Equal(uint32(0), resp.Code, "try-upgrade tx failed with code %d", resp.Code)

	// Wait for one block so that the upgrade transaction is processed
	s.Require().NoError(wait.ForBlocks(ctx, 1, chain))

	// Query upgrade info via gRPC
	s.T().Log("Querying upgrade info via gRPC")
	client, cleanup, err := getSignalQueryClient(validatorNode)
	s.Require().NoError(err)
	defer cleanup()

	upgradeResp, err := client.GetUpgrade(ctx, &signaltypes.QueryGetUpgradeRequest{})
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

	client, cleanup, err := getSignalQueryClient(node)
	s.Require().NoError(err)
	defer cleanup()

	resp, err := client.VersionTally(ctx, &signaltypes.QueryVersionTallyRequest{Version: appVersion})
	s.Require().NoError(err, "failed to query tally")

	// Verify that voting power meets or exceeds threshold
	s.Require().True(resp.VotingPower >= resp.ThresholdPower, "voting power (%d) does not meet threshold (%d)", resp.VotingPower, resp.ThresholdPower)
}

// getSignalQueryClient returns a signaltypes.QueryClient for the provided node.
// If the node is a docker ChainNode with a live *grpc.ClientConn, it is reused.
// Returns an error if no gRPC connection is available.
func getSignalQueryClient(node tastoratypes.ChainNode) (signaltypes.QueryClient, func(), error) {
	if dcNode, ok := node.(*tastoradockertypes.ChainNode); ok && dcNode.GrpcConn != nil {
		return signaltypes.NewQueryClient(dcNode.GrpcConn), func() {}, nil
	}
	return nil, nil, fmt.Errorf("GRPC connection is nil")
}
