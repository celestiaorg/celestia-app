package docker_e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	"github.com/celestiaorg/celestia-app/v5/pkg/appconsts"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
)

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
		baseVersion   = "v4.0.7-mocha"
		targetVersion = "d13d38a"
		targetAppVer  = uint64(5) // The expected app_version after upgrade
		chainID       = appconsts.TestChainID
		homeDir       = "/var/cosmos-chain/celestia"
	)

	cfg := dockerchain.DefaultConfig(s.client, s.network).WithTag(baseVersion)

	kr := cfg.Genesis.Keyring()

	chain, err := dockerchain.NewCelestiaChainBuilder(t, cfg).Build(ctx)
	s.Require().NoError(err)

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

		hostname, err := node.GetInternalHostName(ctx)
		s.Require().NoError(err, "failed to get internal hostname")

		signalCmd := []string{
			"celestia-appd", "tx", "signal", "signal", fmt.Sprintf("%d", targetAppVer),
			"--home", homeDir,
			"--from", records[i].Name,
			"--keyring-backend", "test",
			"--chain-id", chainID,
			"--fees", "200000utia",
			"--node", fmt.Sprintf("tcp://%s:26657", hostname),
			"--yes",
		}
		stdout, stderr, err := node.Exec(ctx, signalCmd, nil)
		s.Require().NoError(err, "failed to signal for upgrade: %s", stderr)
		s.T().Logf("Signal output: %s", stdout)
	}

	s.Require().NoError(wait.ForBlocks(ctx, 2, chain))

	hostname, err := validatorNode.GetInternalHostName(ctx)
	s.Require().NoError(err, "failed to get internal hostname")

	// Query the tally to see if we have enough voting power
	tallyCmd := []string{
		"celestia-appd", "query", "signal", "tally", fmt.Sprintf("%d", targetAppVer),
		"--node", fmt.Sprintf("tcp://%s:26657", hostname),
		"--output", "json",
	}
	tallyStdout, tallyStderr, err := validatorNode.Exec(ctx, tallyCmd, nil)
	s.Require().NoError(err, "failed to query tally: %s", tallyStderr)
	s.T().Logf("Tally output: %s", tallyStdout)

	// Execute try-upgrade transaction on all nodes
	for i, node := range chain.GetNodes() {
		hostname, err := node.GetInternalHostName(ctx)
		s.Require().NoError(err, "failed to get internal hostname")

		s.T().Logf("Executing try-upgrade transaction on validator %d", i)
		tryUpgradeCmd := []string{
			"celestia-appd", "tx", "signal", "try-upgrade",
			"--home", homeDir,
			"--from", records[i].Name,
			"--keyring-backend", "test",
			"--chain-id", chainID,
			"--fees", "200000utia",
			"--node", fmt.Sprintf("tcp://%s:26657", hostname),
			"--yes",
		}
		upgradeStdout, upgradeStderr, err := node.Exec(ctx, tryUpgradeCmd, nil)
		s.Require().NoError(err, "failed to execute try-upgrade on validator %d: %s", i, upgradeStderr)
		s.T().Logf("Try-upgrade output from validator %d: %s", i, upgradeStdout)
	}

	s.T().Log("Querying upgrade info")
	upgradeInfoCmd := []string{
		"celestia-appd", "query", "signal", "upgrade",
		"--node", fmt.Sprintf("tcp://%s:26657", hostname),
		"--output", "json",
	}
	upgradeInfoStdout, upgradeInfoStderr, err := validatorNode.Exec(ctx, upgradeInfoCmd, nil)
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

	// The version string might vary, but should contain the PR number
	versionStr := abciInfo.Response.GetVersion()
	s.Require().True(strings.Contains(versionStr, "d13d38a"), "version should contain PR number")

	// Verify app version is upgraded
	s.Require().Equal(targetAppVer, abciInfo.Response.GetAppVersion(), "app_version mismatch")
}
