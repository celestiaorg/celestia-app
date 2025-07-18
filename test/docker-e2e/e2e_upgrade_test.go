package docker_e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
)

// TallyResponse represents the JSON response from the signal tally query
// We had to define it again because the JSON response type is not a number but string
type TallyResponse struct {
	VotingPower      string `json:"voting_power"`
	ThresholdPower   string `json:"threshold_power"`
	TotalVotingPower string `json:"total_voting_power"`
}

// UpgradeConfig holds configuration parameters for performing an upgrade test
// on the Celestia chain.
type UpgradeConfig struct {
	// BaseBinaryVersion is the initial version of the chain before upgrade.
	BaseBinaryVersion string
	// TargetBinaryVersion is the version to which the chain will be upgraded.
	TargetBinaryVersion string
	// TargetAppVersion is the app version to upgrade to (used in major upgrades).
	TargetAppVersion uint64
}

// TestCelestiaAppMinorUpgrade tests a simple upgrade from one minor version to another.
func (s *CelestiaTestSuite) TestCelestiaAppMinorUpgrade() {
	if testing.Short() {
		s.T().Skip("skipping celestia-app minor upgrade test in short mode")
	}

	s.RunMinorUpgradeTest(UpgradeConfig{
		BaseBinaryVersion:   "v4.0.2-mocha",
		TargetBinaryVersion: "v4.0.7-mocha",
	})
}

// TestCelestiaAppMajorUpgrade tests a major upgrade from v4.0.7-mocha to a commit hash which has v5
// using the signaling mechanism.
func (s *CelestiaTestSuite) TestCelestiaAppMajorUpgrade() {
	if testing.Short() {
		s.T().Skip("skipping celestia-app major upgrade test in short mode")
	}

	s.RunMajorUpgradeTest(UpgradeConfig{
		BaseBinaryVersion:   "v4.0.10",
		TargetBinaryVersion: dockerchain.GetCelestiaTag(),
		TargetAppVersion:    uint64(5),
	})
}

// RunMinorUpgradeTest performs a minor version upgrade test for the Celestia chain (app).
// It starts a chain with the specified base version, performs a bank send transaction to verify functionality,
// upgrades the chain to the target version, restarts it, and verifies that the chain is running the new version
// and that bank send transactions still work.
//
// Example usage:
//
//	s.RunMinorUpgradeTest(UpgradeConfig{
//		BaseVersion:   "v4.0.2-mocha",
//		TargetVersion: "v4.0.7-mocha",
//	})
func (s *CelestiaTestSuite) RunMinorUpgradeTest(upgradeCfg UpgradeConfig) {
	var (
		ctx = context.Background()
		cfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(upgradeCfg.BaseBinaryVersion)
	)

	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
	s.Require().NoError(err)

	// Ensure cleanup at the end of the test
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

	chain.UpgradeVersion(ctx, upgradeCfg.TargetBinaryVersion)

	err = chain.Start(ctx)
	s.Require().NoError(err)

	// Sanity check: Test bank send after upgrade
	s.T().Log("Testing bank send functionality after upgrade")
	testBankSend(s.T(), chain, cfg)

	// Verify the version after upgrade
	validatorNode := chain.GetNodes()[0]

	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client for version check")

	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Contains(abciInfo.Response.GetVersion(), strings.TrimPrefix(upgradeCfg.TargetBinaryVersion, "v"), "version mismatch")
}

// RunMajorUpgradeTest performs an end-to-end test of a major upgrade for the Celestia App chain.
//
// It starts a chain at the specified base version, signals for an upgrade to the target app version
// using the signaling mechanism, ensures the upgrade is scheduled and executed, and verifies that
// the chain is running the new version and app version after the upgrade. The function also performs
// sanity checks before and after the upgrade to ensure basic chain functionality (e.g., bank send).
// It expects the upgrade to be triggered by all validators, and checks that the voting power threshold
// for the upgrade is met before proceeding.
//
// Example usage:
//
//	func (s *CelestiaTestSuite) TestCelestiaAppMajorUpgrade() {
//	    s.RunMajorUpgradeTest(UpgradeConfig{
//	        BaseVersion:      "v4.0.7-mocha",
//	        TargetVersion:    "v5.0.1-mocha",
//	        TargetAppVersion: uint64(5),
//	    })
//	}
func (s *CelestiaTestSuite) RunMajorUpgradeTest(upgradeCfg UpgradeConfig) {
	var (
		ctx = context.Background()
		cfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(upgradeCfg.BaseBinaryVersion)
		kr  = cfg.Genesis.Keyring()
	)

	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
	s.Require().NoError(err)

	// Ensure cleanup at the end of the test
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
	upgradeHeight := s.signalAndGetUpgradeHeight(ctx, chain, validatorNode, records, upgradeCfg.TargetAppVersion)

	// Get current height
	status, err := rpcClient.Status(ctx)
	s.Require().NoError(err, "failed to get node status")
	currentHeight := status.SyncInfo.LatestBlockHeight

	s.T().Logf("Current height: %d, Upgrade height: %d", currentHeight, upgradeHeight)

	// Now simulate the binary upgrade (before the upgrade height is reached)
	// This is what node operators would do in the real world - upgrade their binary
	// before the scheduled upgrade height
	s.T().Log("Stopping the chain to upgrade binaries (simulating node operators upgrading)")
	err = chain.Stop(ctx)
	s.Require().NoError(err)

	// Upgrade to the multiplexer-enabled binary
	chain.UpgradeVersion(ctx, upgradeCfg.TargetBinaryVersion)

	s.T().Log("Restarting the chain with the multiplexer-enabled binary")
	err = chain.Start(ctx)
	s.Require().NoError(err)

	// Verify we're still on the old app version after restart (multiplexer is working)
	rpcClient, err = validatorNode.GetRPCClient()
	s.Require().NoError(err)

	abciInfo, err = rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err)
	s.Require().Equal(currentAppVer, abciInfo.Response.GetAppVersion(),
		"app version should not change immediately after binary upgrade (multiplexer should maintain old version)")

	// Wait until we reach the upgrade height
	blocksToWait := int(upgradeHeight-currentHeight) + 5 // Add buffer
	s.T().Logf("Waiting for %d blocks to reach upgrade height plus buffer", blocksToWait)
	s.Require().NoError(wait.ForBlocks(ctx, blocksToWait, chain))

	// Verify the app version has been upgraded
	abciInfo, err = rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")

	// The version string might vary, but should contain the commit hash
	var (
		versionStr     = abciInfo.Response.GetVersion()
		trimmedVersion = strings.TrimPrefix(upgradeCfg.TargetBinaryVersion, "v")
	)
	s.Require().Contains(versionStr, trimmedVersion, "version should contain %q", trimmedVersion)

	// Verify app version is upgraded
	s.Require().Equal(upgradeCfg.TargetAppVersion, abciInfo.Response.GetAppVersion(), "app_version mismatch")

	// Sanity check: Test bank send after upgrade
	s.T().Log("Testing bank send functionality after upgrade")
	testBankSend(s.T(), chain, cfg)
}

// signalAndGetUpgradeHeight signals for an upgrade to the specified app version and returns the scheduled upgrade height.
func (s *CelestiaTestSuite) signalAndGetUpgradeHeight(ctx context.Context, chain tastoratypes.Chain, validatorNode tastoratypes.ChainNode, records []*keyring.Record, targetAppVersion uint64) int64 {
	// Signal for the upgrade to the new version
	for i, node := range chain.GetNodes() {
		s.T().Logf("Signaling for upgrade to version %d from validator %d", targetAppVersion, i)

		signalCmd := []string{"tx", "signal", "signal", fmt.Sprintf("%d", targetAppVersion), "--from", records[i].Name}
		_, stderr, err := s.ExecuteNodeCommand(ctx, node, signalCmd...)
		s.Require().NoError(err, "failed to signal for upgrade: %s", stderr)
	}

	s.Require().NoError(wait.ForBlocks(ctx, 1, chain))

	s.validateSignalTally(ctx, validatorNode, targetAppVersion)

	// Execute try-upgrade transaction on the first node only
	s.T().Log("Executing try-upgrade transaction on the first validator")
	tryUpgradeCmd := []string{"tx", "signal", "try-upgrade", "--from", records[0].Name}
	_, upgradeStderr, err := s.ExecuteNodeCommand(ctx, validatorNode, tryUpgradeCmd...)
	s.Require().NoError(err, "failed to execute try-upgrade: %s", upgradeStderr)

	s.Require().NoError(wait.ForBlocks(ctx, 1, chain))

	s.T().Log("Querying upgrade info")
	upgradeInfoCmd := []string{"query", "signal", "upgrade", "--output", "json"}
	upgradeInfoStdout, upgradeInfoStderr, err := s.ExecuteNodeCommand(ctx, validatorNode, upgradeInfoCmd...)
	s.Require().NoError(err, "failed to query upgrade info: %s", upgradeInfoStderr)
	s.T().Logf("Upgrade info: %s", upgradeInfoStdout)

	// Parse the upgrade height from the plain text response
	// it turns out that the json output flag is not working as expected
	// so we need to parse the plain text response
	// Expected format: "An upgrade is pending to app version X at height Y."
	var upgradeHeight int64

	if strings.Contains(upgradeInfoStdout, "An upgrade is pending") {
		// Extract the height from the response using regex
		re := regexp.MustCompile(`height (\d+)`)
		matches := re.FindStringSubmatch(upgradeInfoStdout)
		s.Require().GreaterOrEqual(len(matches), 2, "could not extract height from response: %s", upgradeInfoStdout)

		var err error
		upgradeHeight, err = strconv.ParseInt(matches[1], 10, 64)
		s.Require().NoError(err, "failed to parse upgrade height from string: %s", matches[1])
		return upgradeHeight
	}

	// Try to parse as JSON as a fallback
	var upgradeInfo struct {
		Height string `json:"height"`
	}
	err = json.Unmarshal([]byte(upgradeInfoStdout), &upgradeInfo)
	s.Require().NoError(err, "failed to parse upgrade info")

	// Ensure we got a valid height
	s.Require().NotEmpty(upgradeInfo.Height, "upgrade height should not be empty")

	upgradeHeight, err = strconv.ParseInt(upgradeInfo.Height, 10, 64)
	s.Require().NoError(err, "failed to parse upgrade height")

	return upgradeHeight
}

// validateSignalTally queries the signal tally for the given app version and verifies
// that the voting power meets or exceeds the threshold power.
func (s *CelestiaTestSuite) validateSignalTally(ctx context.Context, node tastoratypes.ChainNode, appVersion uint64) {
	s.T().Logf("Querying signal tally for app version %d", appVersion)

	tallyCmd := []string{"query", "signal", "tally", fmt.Sprintf("%d", appVersion), "--output", "json"}
	tallyStdout, tallyStderr, err := s.ExecuteNodeCommand(ctx, node, tallyCmd...)
	s.Require().NoError(err, "failed to query tally: %s", tallyStderr)

	var tally TallyResponse
	s.Require().NoError(json.Unmarshal([]byte(tallyStdout), &tally), "failed to parse tally response")

	// Convert the string values to integers
	votingPower, err := strconv.ParseInt(tally.VotingPower, 10, 64)
	s.Require().NoError(err, "failed to parse voting power")

	thresholdPower, err := strconv.ParseInt(tally.ThresholdPower, 10, 64)
	s.Require().NoError(err, "failed to parse threshold power")

	// Verify that voting power meets or exceeds threshold
	s.Require().True(votingPower >= thresholdPower, "voting power (%d) does not meet threshold (%d)", votingPower, thresholdPower)
}
