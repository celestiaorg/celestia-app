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

// TestCelestiaAppMinorUpgrade tests a simple upgrade from one minor version to another by swapping the binary.
func (s *CelestiaTestSuite) TestCelestiaAppMinorUpgrade() {
	if testing.Short() {
		s.T().Skip("skipping celestia-app minor upgrade test in short mode")
	}

	tt := []struct {
		Name                string
		BaseBinaryVersion   string
		TargetBinaryVersion string
	}{
		{Name: "v4.0.2-rc2 to v4.0.10", BaseBinaryVersion: "v4.0.2-rc2", TargetBinaryVersion: "v4.0.10"},
		{Name: "v4.0.9-mocha to v4.0.10-mocha", BaseBinaryVersion: "v4.0.9-mocha", TargetBinaryVersion: "v4.0.10-mocha"},
		{Name: "v4.0.9-arabica to v4.0.10-arabica", BaseBinaryVersion: "v4.0.9-arabica", TargetBinaryVersion: "v4.0.10-arabica"},
	}

	for _, tc := range tt {
		s.Run(tc.Name, func() {
			s.RunMinorUpgradeTest(tc.BaseBinaryVersion, tc.TargetBinaryVersion)
		})
	}
}

// TestCelestiaAppMajorUpgrade tests a major upgrade using the signaling mechanism.
func (s *CelestiaTestSuite) TestCelestiaAppMajorUpgrade() {
	if testing.Short() {
		s.T().Skip("skipping celestia-app major upgrade test in short mode")
	}

	tt := []struct {
		Name             string
		BinaryVersion    string
		TargetAppVersion uint64
	}{
		{
			Name:             "v2 to v3",
			BinaryVersion:    dockerchain.GetCelestiaTag(),
			TargetAppVersion: 3,
		},
		{
			Name:             "v3 to v4",
			BinaryVersion:    dockerchain.GetCelestiaTag(),
			TargetAppVersion: 4,
		},
	}

	for _, tc := range tt {
		s.Run(tc.Name, func() {
			s.RunMajorUpgradeTest(tc.BinaryVersion, tc.TargetAppVersion)
		})
	}
}

// RunMinorUpgradeTest performs a minor version upgrade test for the celestia-app.
// It starts a chain with the specified base version, performs a bank send transaction to verify functionality,
// upgrades the chain to the target version, restarts it, and verifies that the chain is running the new version
// and that bank send transactions still work.
//
// Example usage:
//
//	s.RunMinorUpgradeTest("v4.0.2-mocha", "v4.0.10-mocha")
func (s *CelestiaTestSuite) RunMinorUpgradeTest(BaseBinaryVersion, TargetBinaryVersion string) {
	var (
		ctx = context.Background()
		cfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(BaseBinaryVersion)
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

	chain.UpgradeVersion(ctx, TargetBinaryVersion)

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
	s.Require().Contains(abciInfo.Response.GetVersion(), strings.TrimPrefix(TargetBinaryVersion, "v"), "version mismatch")
}

// RunMajorUpgradeTest performs an end-to-end test of a major upgrade for the celestia-app.
// It starts a chain at the specified binary version, sets the app version to one less than the target,
// and then signals for an upgrade to the target app version using the signaling mechanism.
// The function ensures the upgrade is scheduled and executed, verifies that the chain is running the new binary
// and app version after the upgrade, and performs sanity checks (such as bank send) before and after the upgrade.
// It requires all validators to signal for the upgrade and checks that the voting power threshold is met before proceeding.
//
// Example usage:
//
//	s.RunMajorUpgradeTest("v4.0.10-mocha", 5)
func (s *CelestiaTestSuite) RunMajorUpgradeTest(BinaryVersion string, TargetAppVersion uint64) {
	// Since the siganling mechanism was introduced in v2, we need to ensure that the target app version is greater than 2
	s.Require().Greater(TargetAppVersion, uint64(2), "target app version must be greater than 2")

	var (
		ctx = context.Background()
		cfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(BinaryVersion)
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
