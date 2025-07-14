package docker_e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	"github.com/celestiaorg/celestia-app/v5/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v5/test/util/genesis"
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
		baseVersion     = "v4.0.7-mocha"
		targetVersion   = "d13d38a"
		targetAppVer    = uint64(5) // The expected app_version after upgrade
		chainID         = appconsts.TestChainID
		validatorsCount = 4
	)

	cfg := dockerchain.DefaultConfig(s.client, s.network)
	cfg.Tag = baseVersion

	validatorNames := make([]string, validatorsCount)
	for i := range validatorsCount {
		validatorNames[i] = fmt.Sprintf("val-%d", i)
	}

	emptyGenesis := genesis.NewDefaultGenesis().WithChainID(chainID)

	for _, name := range validatorNames {
		val := genesis.NewDefaultValidator(name)
		val.KeyringAccount.Name = name
		emptyGenesis = emptyGenesis.WithValidators(val)
	}
	cfg.Config.Genesis = emptyGenesis

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
	fmt.Printf("\n\n\t\tBEFORE: %v\n", currentAppVer)

	// Signal for the upgrade to version 5 for each validator
	for i, node := range chain.GetNodes() {
		s.T().Logf("Signaling for upgrade to version %d from validator %d", targetAppVer, i)

		signalCmd := []string{
			"tx", "signal", "signal", fmt.Sprintf("%d", targetAppVer),
			"--from", validatorNames[i],
			"--keyring-backend", "test",
			"--chain-id", chainID,
			"--fees", "200000utia",
			"--yes",
		}
		stdout, stderr, err := node.ExecBinInContainer(ctx, signalCmd...)
		s.Require().NoError(err, "failed to signal for upgrade: %s", stderr)
		s.T().Logf("Signal output: %s", stdout)

		// Wait a bit between signals to avoid transaction conflicts
		time.Sleep(1 * time.Second)
	}

	time.Sleep(2 * time.Second)

	// Query the tally to see if we have enough voting power
	tallyCmd := []string{
		"query", "signal", "tally", fmt.Sprintf("%d", targetAppVer),
		"--output", "json",
	}
	tallyStdout, tallyStderr, err := validatorNode.ExecBinInContainer(ctx, tallyCmd...)
	s.Require().NoError(err, "failed to query tally: %s", tallyStderr)
	s.T().Logf("Tally output: %s", tallyStdout)

	// Execute try-upgrade transaction on all nodes
	for i, node := range chain.GetNodes() {
		s.T().Logf("Executing try-upgrade transaction on validator %d", i)
		tryUpgradeCmd := []string{
			"tx", "signal", "try-upgrade",
			"--from", validatorNames[i],
			"--keyring-backend", "test",
			"--chain-id", chainID,
			"--fees", "200000utia",
			"--yes",
		}
		upgradeStdout, upgradeStderr, err := node.ExecBinInContainer(ctx, tryUpgradeCmd...)
		s.Require().NoError(err, "failed to execute try-upgrade on validator %d: %s", i, upgradeStderr)
		s.T().Logf("Try-upgrade output from validator %d: %s", i, upgradeStdout)

		// Wait a bit between transactions to avoid conflicts
		time.Sleep(1 * time.Second)
	}

	s.T().Log("Querying upgrade info")
	upgradeInfoCmd := []string{
		"query", "signal", "upgrade",
		"--output", "json",
	}
	upgradeInfoStdout, upgradeInfoStderr, err := validatorNode.ExecBinInContainer(ctx, upgradeInfoCmd...)
	s.Require().NoError(err, "failed to query upgrade info: %s", upgradeInfoStderr)
	s.T().Logf("Upgrade info: %s", upgradeInfoStdout)

	// Wait for the upgrade to be scheduled
	time.Sleep(5 * time.Second)

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
	fmt.Printf("\n\n\t\tversionStr: %v\n", versionStr)
	fmt.Printf("\n\n\t\tAFTERabciInfo.Response.GetAppVersion(): %v\n", abciInfo.Response.GetAppVersion())
	s.Require().True(strings.Contains(versionStr, "d13d38a"), "version should contain PR number")

	// Verify app version is upgraded
	s.Require().Equal(targetAppVer, abciInfo.Response.GetAppVersion(), "app_version mismatch")
}
