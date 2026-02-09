package docker_e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	"github.com/celestiaorg/celestia-app/v7/app"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/pkg/user"
	signaltypes "github.com/celestiaorg/celestia-app/v7/x/signal/types"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	AppVersionV5 uint64 = 5
	AppVersionV6 uint64 = 6
	AppVersionV7 uint64 = 7

	InflationRateV5 = "0.0536" // 5.36%
	InflationRateV6 = "0.0267" // 2.67%

	UnbondingTimeV5Hours = 504
	UnbondingTimeV6Hours = 337

	MinCommissionRateV5 = "0.05" // 5%
	MinCommissionRateV6 = "0.10" // 10%
	MinCommissionRateV7 = "0.20" // 20%

	EvidenceMaxAgeV5Hours = 504
	EvidenceMaxAgeV6Hours = 337

	EvidenceMaxAgeV5Blocks = 120960
	EvidenceMaxAgeV6Blocks = 242640
)

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
		{
			baseAppVersion:   6,
			targetAppVersion: 7,
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
		if err := chain.Remove(ctx); err != nil {
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
	upgradeHeight := s.SignalUpgrade(ctx, chain, validatorNode, cfg, records, targetAppVersion)

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
	s.Require().NoError(s.CheckLiveness(ctx, chain), "validator liveness check failed")
}

// TestUpgradeLatest tests the most current version upgrade.
// This test should include any assertions relevant to the current version upgrade.
func (s *CelestiaTestSuite) TestUpgradeLatest() {
	if testing.Short() {
		s.T().Skip("skipping latest upgrade test in short mode")
	}

	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err)

	cfg := dockerchain.DefaultConfig(s.client, s.network).WithTag(tag)
	cfg.Genesis = cfg.Genesis.WithAppVersion(appconsts.Version - 1)

	ctx := context.Background()
	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			s.T().Logf("Error removing chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err)

	s.ValidatePreUpgrade(ctx, chain, cfg)
	s.UpgradeChain(ctx, chain, cfg, appconsts.Version)
	s.ValidatePostUpgrade(ctx, chain, cfg)
}

// UpgradeChain executes the upgrade to the target app version.
func (s *CelestiaTestSuite) UpgradeChain(ctx context.Context, chain tastoratypes.Chain, cfg *dockerchain.Config, appVersion uint64) (upgradeHeight int64) {
	t := s.T()

	validatorNode := chain.GetNodes()[0]
	kr := cfg.Genesis.Keyring()
	records, err := kr.List()
	s.Require().NoError(err, "failed to list keyring records")
	s.Require().Len(records, len(chain.GetNodes()), "number of accounts should match number of nodes")

	upgradeHeight = s.SignalUpgrade(ctx, chain, validatorNode, cfg, records, appVersion)

	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	currentHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get current height")

	blocksToWait := int(upgradeHeight-currentHeight) + 2
	t.Logf("Waiting for %d blocks to reach upgrade height %d", blocksToWait, upgradeHeight)
	s.Require().NoError(wait.ForBlocks(ctx, blocksToWait, chain), "failed to wait for upgrade completion")

	// Verify upgrade completed successfully
	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(appVersion, abciInfo.Response.GetAppVersion(), "should be at app version %v", appVersion)

	// Produce additional blocks at the target app version (TxSim is still running)
	t.Logf("Producing 20 more blocks at app version %v", appVersion)
	s.Require().NoError(wait.ForBlocks(ctx, 20, chain), "failed to wait for post-upgrade blocks")

	return upgradeHeight
}

// SignalUpgrade signals for an upgrade to the specified app version and returns the scheduled upgrade height.
func (s *CelestiaTestSuite) SignalUpgrade(
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

func (s *CelestiaTestSuite) ValidatePreUpgrade(ctx context.Context, chain tastoratypes.Chain, cfg *dockerchain.Config) {
	appVersion := appconsts.Version - 1

	node := chain.GetNodes()[0]
	rpcClient, err := node.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(appVersion, abciInfo.Response.GetAppVersion(), "should be running v%d", appVersion)
}

func (s *CelestiaTestSuite) ValidatePostUpgrade(ctx context.Context, chain tastoratypes.Chain, cfg *dockerchain.Config) {
	appVersion := appconsts.Version

	node := chain.GetNodes()[0]
	rpcClient, err := node.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(appVersion, abciInfo.Response.GetAppVersion(), "should be running v%d", appVersion)
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

// getICAHostQueryClient returns an icahosttypes.QueryClient for the provided node.
// If the node is a docker ChainNode with a live *grpc.ClientConn, it is reused.
// Returns an error if no gRPC connection is available.
func getICAHostQueryClient(node tastoratypes.ChainNode) (icahosttypes.QueryClient, error) {
	if dcNode, ok := node.(*tastoradockertypes.ChainNode); ok && dcNode.GrpcConn != nil {
		return icahosttypes.NewQueryClient(dcNode.GrpcConn), nil
	}
	return nil, fmt.Errorf("GRPC connection is nil")
}

// validateParameters validates that all parameters match expected values for the given app version
// TODO: Refactor or remove entirely. Currently this method deals with app version 5->6, only.
// This method is currently unused.
func (s *CelestiaTestSuite) validateParameters(ctx context.Context, node tastoratypes.ChainNode, appVersion uint64) {
	// Verify we're running the correct app version
	rpcClient, err := node.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(appVersion, abciInfo.Response.GetAppVersion(), "should be running v%d", appVersion)

	if appVersion == AppVersionV5 {
		s.validateInflationRate(ctx, node, InflationRateV5, AppVersionV5)
		s.validateUnbondingTime(ctx, node, UnbondingTimeV5Hours, AppVersionV5)
		s.validateMinCommissionRate(ctx, node, MinCommissionRateV5, AppVersionV5)
		s.validateEvidenceParams(ctx, node, EvidenceMaxAgeV5Hours, EvidenceMaxAgeV5Blocks, AppVersionV5)
		return
	}

	if appVersion == AppVersionV6 {
		s.validateInflationRate(ctx, node, InflationRateV6, AppVersionV6)
		s.validateUnbondingTime(ctx, node, UnbondingTimeV6Hours, AppVersionV6)
		s.validateMinCommissionRate(ctx, node, MinCommissionRateV6, AppVersionV6)
		s.validateEvidenceParams(ctx, node, EvidenceMaxAgeV6Hours, EvidenceMaxAgeV6Blocks, AppVersionV6)
		// Check ICA host params only on v6: v5 doesn't expose the icahost gRPC query service;
		// the v6 upgrade applies these params per CIP-14.
		s.validateICAHostParams(ctx, node, true, app.IcaAllowMessages(), AppVersionV6)
		return
	}

	if appVersion == AppVersionV7 {
		s.validateMinCommissionRate(ctx, node, MinCommissionRateV7, AppVersionV7)
		return
	}

	s.T().Fatalf("invalid app version: %d", appVersion)
}

// validateInflationRate queries and validates the current inflation rate using CLI
func (s *CelestiaTestSuite) validateInflationRate(ctx context.Context, node tastoratypes.ChainNode, expectedRate string, appVersion uint64) {
	dcNode, ok := node.(*tastoradockertypes.ChainNode)
	s.Require().True(ok, "node should be a docker chain node")

	networkInfo, err := node.GetNetworkInfo(ctx)
	s.Require().NoError(err, "failed to get network info from chain node")

	rpcEndpoint := fmt.Sprintf("tcp://%s:26657", networkInfo.Internal.Hostname)
	cmd := []string{"celestia-appd", "query", "mint", "inflation", "--node", rpcEndpoint}

	stdout, stderr, err := dcNode.Exec(ctx, cmd, nil)
	s.Require().NoError(err, "failed to query inflation rate via CLI: stderr=%s", string(stderr))

	inflationRateStr := strings.TrimSpace(string(stdout))
	actualDec, err := math.LegacyNewDecFromStr(inflationRateStr)
	s.Require().NoError(err, "failed to parse actual inflation rate")

	expectedDec, err := math.LegacyNewDecFromStr(expectedRate)
	s.Require().NoError(err, "failed to parse expected inflation rate")

	// Use tolerance-based comparison for floating-point precision
	tolerance := math.LegacyNewDecWithPrec(1, 10)
	diff := actualDec.Sub(expectedDec).Abs()
	s.Require().True(diff.LTE(tolerance), "%d inflation rate mismatch: expected %s, got %s, diff=%s", appVersion, expectedRate, inflationRateStr, diff.String())
}

// validateUnbondingTime queries and validates the current unbonding time using CLI
func (s *CelestiaTestSuite) validateUnbondingTime(ctx context.Context, node tastoratypes.ChainNode, expectedHours int, appVersion uint64) {
	dcNode, ok := node.(*tastoradockertypes.ChainNode)
	s.Require().True(ok, "node should be a docker chain node")

	networkInfo, err := node.GetNetworkInfo(ctx)
	s.Require().NoError(err, "failed to get network info from chain node")

	rpcEndpoint := fmt.Sprintf("tcp://%s:26657", networkInfo.Internal.Hostname)
	cmd := []string{"celestia-appd", "query", "staking", "params", "--output", "json", "--node", rpcEndpoint}

	stdout, stderr, err := dcNode.Exec(ctx, cmd, nil)
	s.Require().NoError(err, "failed to query staking params via CLI: stderr=%s", string(stderr))

	var stakingParams struct {
		Params struct {
			UnbondingTime string `json:"unbonding_time"`
		} `json:"params"`
	}
	err = json.Unmarshal(stdout, &stakingParams)
	s.Require().NoError(err, "failed to parse staking params JSON response")

	unbondingTimeStr := stakingParams.Params.UnbondingTime
	s.Require().NotEmpty(unbondingTimeStr, "unbonding_time not found in response")

	actualDuration, err := time.ParseDuration(unbondingTimeStr)
	s.Require().NoError(err, "failed to parse unbonding time duration: %s", unbondingTimeStr)

	expectedDuration := time.Duration(expectedHours) * time.Hour
	s.Require().Equal(expectedDuration, actualDuration, "v%d unbonding time mismatch: expected %v (%d hours), got %v", appVersion, expectedDuration, expectedHours, actualDuration)
}

// validateMinCommissionRate queries and validates the current minimum commission rate using CLI
func (s *CelestiaTestSuite) validateMinCommissionRate(ctx context.Context, node tastoratypes.ChainNode, expectedRate string, appVersion uint64) {
	dcNode, ok := node.(*tastoradockertypes.ChainNode)
	s.Require().True(ok, "node should be a docker chain node")

	networkInfo, err := node.GetNetworkInfo(ctx)
	s.Require().NoError(err, "failed to get network info from chain node")

	rpcEndpoint := fmt.Sprintf("tcp://%s:26657", networkInfo.Internal.Hostname)
	cmd := []string{"celestia-appd", "query", "staking", "params", "--output", "json", "--node", rpcEndpoint}

	stdout, stderr, err := dcNode.Exec(ctx, cmd, nil)
	s.Require().NoError(err, "failed to query staking params via CLI: stderr=%s", string(stderr))

	var stakingParams struct {
		Params struct {
			MinCommissionRate string `json:"min_commission_rate"`
		} `json:"params"`
	}
	err = json.Unmarshal(stdout, &stakingParams)
	s.Require().NoError(err, "failed to parse staking params JSON response")

	minCommissionRateStr := stakingParams.Params.MinCommissionRate
	s.Require().NotEmpty(minCommissionRateStr, "min_commission_rate not found in response")

	actualDec, err := math.LegacyNewDecFromStr(minCommissionRateStr)
	s.Require().NoError(err, "failed to parse actual min commission rate: %s", minCommissionRateStr)

	expectedDec, err := math.LegacyNewDecFromStr(expectedRate)
	s.Require().NoError(err, "failed to parse expected min commission rate")

	// Use tolerance-based comparison for floating-point precision
	tolerance := math.LegacyNewDecWithPrec(1, 10)
	diff := actualDec.Sub(expectedDec).Abs()
	s.Require().True(diff.LTE(tolerance), "v%d min commission rate mismatch: expected %s, got %s, diff=%s", appVersion, expectedRate, minCommissionRateStr, diff.String())
}

// validateEvidenceParams queries and validates both evidence max age duration and blocks using CLI
func (s *CelestiaTestSuite) validateEvidenceParams(ctx context.Context, node tastoratypes.ChainNode, expectedHours int, expectedBlocks int, appVersion uint64) {
	dcNode, ok := node.(*tastoradockertypes.ChainNode)
	s.Require().True(ok, "node should be a docker chain node")

	networkInfo, err := node.GetNetworkInfo(ctx)
	s.Require().NoError(err, "failed to get network info from chain node")

	rpcEndpoint := fmt.Sprintf("tcp://%s:26657", networkInfo.Internal.Hostname)
	cmd := []string{"celestia-appd", "query", "consensus", "params", "--output", "json", "--node", rpcEndpoint}

	stdout, stderr, err := dcNode.Exec(ctx, cmd, nil)
	s.Require().NoError(err, "failed to query consensus params via CLI: stderr=%s", string(stderr))

	var consensusParams struct {
		Params struct {
			Evidence struct {
				MaxAgeDuration  string `json:"max_age_duration"`
				MaxAgeNumBlocks string `json:"max_age_num_blocks"`
			} `json:"evidence"`
		} `json:"params"`
	}
	err = json.Unmarshal(stdout, &consensusParams)
	s.Require().NoError(err, "failed to parse consensus params JSON response")

	maxAgeDurationStr := consensusParams.Params.Evidence.MaxAgeDuration
	s.Require().NotEmpty(maxAgeDurationStr, "max_age_duration not found in response")

	actualDuration, err := time.ParseDuration(maxAgeDurationStr)
	s.Require().NoError(err, "failed to parse evidence max age duration: %s", maxAgeDurationStr)

	expectedDuration := time.Duration(expectedHours) * time.Hour
	s.Require().Equal(expectedDuration, actualDuration, "v%d evidence max age duration mismatch: expected %v (%d hours), got %v", appVersion, expectedDuration, expectedHours, actualDuration)

	maxAgeNumBlocksStr := consensusParams.Params.Evidence.MaxAgeNumBlocks
	s.Require().NotEmpty(maxAgeNumBlocksStr, "max_age_num_blocks not found in response")

	actualBlocks, err := strconv.Atoi(maxAgeNumBlocksStr)
	s.Require().NoError(err, "failed to parse evidence max age num blocks: %s", maxAgeNumBlocksStr)

	s.Require().Equal(expectedBlocks, actualBlocks, "v%d evidence max age num blocks mismatch: expected %d, got %d", appVersion, expectedBlocks, actualBlocks)
}

// validateICAHostParams queries and validates ICA host params (host_enabled and allow_messages) via gRPC
func (s *CelestiaTestSuite) validateICAHostParams(ctx context.Context, node tastoratypes.ChainNode, expectedHostEnabled bool, expectedAllowMessages []string, appVersion uint64) {
	client, err := getICAHostQueryClient(node)
	s.Require().NoError(err)

	resp, err := client.Params(ctx, &icahosttypes.QueryParamsRequest{})
	s.Require().NoError(err, "failed to query ICA host params")

	s.Require().Equal(expectedHostEnabled, resp.Params.HostEnabled, "v%d icahost host_enabled mismatch: expected %v, got %v", appVersion, expectedHostEnabled, resp.Params.HostEnabled)
	s.Require().Equal(expectedAllowMessages, resp.Params.AllowMessages, "v%d icahost allow_messages mismatch", appVersion)
}
