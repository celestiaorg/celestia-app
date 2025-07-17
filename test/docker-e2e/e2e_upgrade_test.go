package docker_e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
)

// TallyResponse represents the JSON response from the signal tally query
// We had to define it again because the JSON response type is not a number but string
type TallyResponse struct {
	VotingPower      string `json:"voting_power"`
	ThresholdPower   string `json:"threshold_power"`
	TotalVotingPower string `json:"total_voting_power"`
}

// UpgradeConfig defines the configuration parameters for performing an upgrade test
// on the Celestia chain. It specifies the base version to start from, the target
// version to upgrade to, and the target app version (used for major upgrades).
type UpgradeConfig struct {
	BaseVersion      string // BaseVersion is the initial version of the chain before upgrade.
	TargetVersion    string // TargetVersion is the version to which the chain will be upgraded.
	TargetAppVersion uint64 // TargetAppVersion is the app version to upgrade to (used in major upgrades).
}

// TestCelestiaAppMinorUpgrade tests a simple upgrade from one minor version to another.
func (s *CelestiaTestSuite) TestCelestiaAppMinorUpgrade() {
	if testing.Short() {
		s.T().Skip("skipping upgrade test in short mode")
	}

	s.RunMinorUpgradeTest(UpgradeConfig{
		BaseVersion:   "v4.0.2-mocha",
		TargetVersion: "v4.0.7-mocha",
	})
}

// TestCelestiaAppMajorUpgrade tests a major upgrade from v4.0.7-mocha to a commit hash which has v5
// using the signaling mechanism.
func (s *CelestiaTestSuite) TestCelestiaAppMajorUpgrade() {
	if testing.Short() {
		s.T().Skip("skipping major upgrade test in short mode")
	}

	s.RunMajorUpgradeTest(UpgradeConfig{
		BaseVersion:      "v4.0.7-mocha",
		TargetVersion:    dockerchain.GetCelestiaTag(),
		TargetAppVersion: uint64(5),
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
		cfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(upgradeCfg.BaseVersion)
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

	chain.UpgradeVersion(ctx, upgradeCfg.TargetVersion)

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
	s.Require().Contains(abciInfo.Response.GetVersion(), strings.TrimPrefix(upgradeCfg.TargetVersion, "v"), "version mismatch")
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
//
// Arguments:
//   - upgradeCfg: UpgradeConfig struct specifying the base version, target version, and target app version.
func (s *CelestiaTestSuite) RunMajorUpgradeTest(upgradeCfg UpgradeConfig) {
	var (
		ctx = context.Background()
		cfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(upgradeCfg.BaseVersion)
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

	// Signal for the upgrade to the new version
	for i, node := range chain.GetNodes() {
		s.T().Logf("Signaling for upgrade to version %d from validator %d", upgradeCfg.TargetAppVersion, i)

		signalCmd := []string{"tx", "signal", "signal", fmt.Sprintf("%d", upgradeCfg.TargetAppVersion), "--from", records[i].Name}
		_, stderr, err := s.ExecuteNodeCommand(ctx, node, signalCmd...)
		s.Require().NoError(err, "failed to signal for upgrade: %s", stderr)
	}

	s.Require().NoError(wait.ForBlocks(ctx, 2, chain))

	s.validateSignalTally(ctx, validatorNode, upgradeCfg.TargetAppVersion)

	// Execute try-upgrade transaction on all nodes
	for i, node := range chain.GetNodes() {
		s.T().Logf("Executing try-upgrade transaction on validator %d", i)
		tryUpgradeCmd := []string{"tx", "signal", "try-upgrade", "--from", records[i].Name}
		_, upgradeStderr, err := s.ExecuteNodeCommand(ctx, node, tryUpgradeCmd...)
		s.Require().NoError(err, "failed to execute try-upgrade on validator %d: %s", i, upgradeStderr)
	}

	s.T().Log("Querying upgrade info")
	upgradeInfoCmd := []string{"query", "signal", "upgrade", "--output", "json"}
	upgradeInfoStdout, upgradeInfoStderr, err := s.ExecuteNodeCommand(ctx, validatorNode, upgradeInfoCmd...)
	s.Require().NoError(err, "failed to query upgrade info: %s", upgradeInfoStderr)
	s.T().Logf("Upgrade info: %s", upgradeInfoStdout)

	// Wait for the upgrade to be scheduled
	s.Require().NoError(wait.ForBlocks(ctx, 5, chain))

	err = chain.Stop(ctx)
	s.Require().NoError(err)

	chain.UpgradeVersion(ctx, upgradeCfg.TargetVersion)

	err = chain.Start(ctx)
	s.Require().NoError(err)

	// Verify the version after upgrade
	rpcClient, err = validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client for version check")

	abciInfo, err = rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")

	// The version string might vary, but should contain the commit hash
	var (
		versionStr     = abciInfo.Response.GetVersion()
		trimmedVersion = strings.TrimPrefix(upgradeCfg.TargetVersion, "v")
	)
	s.Require().Contains(versionStr, trimmedVersion, "version should contain %q", trimmedVersion)

	// Verify app version is upgraded
	s.Require().Equal(upgradeCfg.TargetAppVersion, abciInfo.Response.GetAppVersion(), "app_version mismatch")

	// Sanity check: Test bank send after upgrade
	s.T().Log("Testing bank send functionality after upgrade")
	testBankSend(s.T(), chain, cfg)
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
