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

// TestCelestiaAppMinorUpgrade tests a simple upgrade from one minor version to another.
func (s *CelestiaTestSuite) TestCelestiaAppMinorUpgrade() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping upgrade test in short mode")
	}

	ctx := context.Background()
	const (
		baseVersion   = "v4.0.2-mocha"
		targetVersion = "v4.0.7-mocha"
	)

	cfg := dockerchain.DefaultConfig(s.client, s.network)
	cfg.Tag = baseVersion
	builder := dockerchain.NewCelestiaChainBuilder(t, cfg)

	chain, err := builder.Build(ctx)
	s.Require().NoError(err)

	// Ensure cleanup at the end of the test
	t.Cleanup(func() {
		if err := chain.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err)

	s.Require().NoError(wait.ForBlocks(ctx, 5, chain))

	err = chain.Stop(ctx)
	s.Require().NoError(err)

	chain.UpgradeVersion(ctx, targetVersion)

	err = chain.Start(ctx)
	s.Require().NoError(err)

	err = wait.ForBlocks(ctx, 2, chain)
	s.Require().NoError(err)

	// Verify the version after upgrade
	validatorNode := chain.GetNodes()[0]

	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client for version check")

	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(strings.TrimPrefix(targetVersion, "v"), abciInfo.Response.GetVersion(), "version mismatch")
}

// TestCelestiaAppMajorUpgrade tests a major upgrade from v4.0.7-mocha to a commit hash which has v5
// using the signaling mechanism.
func (s *CelestiaTestSuite) TestCelestiaAppMajorUpgrade() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping major upgrade test in short mode")
	}

	ctx := context.Background()
	const (
		baseVersion  = "v4.0.7-mocha"
		targetAppVer = uint64(5) // The expected app_version after upgrade
	)
	targetVersion := dockerchain.GetCelestiaTag()

	cfg := dockerchain.DefaultConfig(s.client, s.network).WithTag(baseVersion)

	kr := cfg.Genesis.Keyring()

	chain, err := dockerchain.NewCelestiaChainBuilder(t, cfg).Build(ctx)
	s.Require().NoError(err)

	// Ensure cleanup at the end of the test
	t.Cleanup(func() {
		if err := chain.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err)

	s.Require().NoError(wait.ForBlocks(ctx, 5, chain))

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

	// Signal for the upgrade to version 5 for each validator
	for i, node := range chain.GetNodes() {
		s.T().Logf("Signaling for upgrade to version %d from validator %d", targetAppVer, i)

		signalCmd := []string{"tx", "signal", "signal", fmt.Sprintf("%d", targetAppVer), "--from", records[i].Name}
		_, stderr, err := s.ExecuteNodeCommand(ctx, node, signalCmd...)
		s.Require().NoError(err, "failed to signal for upgrade: %s", stderr)
	}

	s.Require().NoError(wait.ForBlocks(ctx, 2, chain))

	s.validateSignalTally(ctx, validatorNode, targetAppVer)

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

	chain.UpgradeVersion(ctx, targetVersion)

	err = chain.Start(ctx)
	s.Require().NoError(err)

	err = wait.ForBlocks(ctx, 2, chain)
	s.Require().NoError(err)

	// Verify the version after upgrade
	rpcClient, err = validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client for version check")

	abciInfo, err = rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")

	// The version string might vary, but should contain the commit hash
	versionStr := abciInfo.Response.GetVersion()
	s.Require().True(strings.Contains(versionStr, strings.TrimPrefix(targetVersion, "v")), "version should contain commit hash")

	// Verify app version is upgraded
	s.Require().Equal(targetAppVer, abciInfo.Response.GetAppVersion(), "app_version mismatch")
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
	s.Assert().True(votingPower >= thresholdPower, "voting power (%d) does not meet threshold (%d)", votingPower, thresholdPower)
}
