package docker_e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/tastora/framework/docker/container"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/docker/hyperlane"
	"github.com/celestiaorg/tastora/framework/testutil/evm"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	ethcommon "github.com/ethereum/go-ethereum/common"

	"celestiaorg/celestia-app/test/docker-e2e/internal"
)

// hypValidatorKey is the key of the Hyperlane validator that signs Celestia
// checkpoints in the multisig tests; the EVM mailbox's default ISM is a 1-of-1
// messageIdMultisigIsm of its address.
const hypValidatorKey = "0x8b3a350cf5c34c9194ca85829a2df0ec3153be0318b5e2d3348e872092edffba"

// TestHyperlaneMultisigExportRestart restarts Celestia from an export at the
// last height while the EVM mailbox accepts Celestia messages only with the
// validator's signed checkpoints. The validator keeps its database and
// checkpoints across the restart: no merkle tree index is reused, so it signs
// the messages of the new chain.
func (s *HyperlaneTestSuite) TestHyperlaneMultisigExportRestart() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping hyperlane multisig export restart test in short mode")
	}
	ctx := context.Background()

	h := s.setupHyperlaneExportWith(ctx, hypValidatorKey)
	rethRecipient := ethcommon.HexToAddress("0xaF9053bB6c4346381C77C2FeD279B17ABAfCDf4d")
	sendToEVM := func(amount int64) { s.sendToEVMViaMerkleTree(ctx, h, rethRecipient, amount) }
	requireEVMBalance := func(want int64) {
		s.AssertERC20Balance(ctx, h.reth, h.tokenRouter, rethRecipient, big.NewInt(want))
	}

	sendToEVM(1000)
	requireEVMBalance(1000)

	// Sent while the relayer is stopped; the validator signs it before the halt.
	s.Require().NoError(h.agent.Stop(ctx), "failed to stop the relayer")
	sendToEVM(300)
	s.waitForCheckpoint(ctx, h.deployer, 1)
	s.Require().NoError(h.validator.RemoveContainer(ctx, tastoratypes.WithPreserveVolumes()), "failed to stop the validator")

	r := &exportRestart{archive: h.archive}
	s.exportAndRestart(ctx, h.chain, r)
	t.Logf("exported at height %d, new chain starts at %d", r.exportHeight, r.initialHeight)

	// The pending message is in the old blocks: the relayer reads them from
	// the archive, then follows the new chain, with one database.
	validatorNode := h.chain.Validators[0]
	s.restartRelayerOn(ctx, h.agent, h.deployer, validatorNode, h.archive, 0, relayerExportDB)
	s.Run("pending message delivered", func() { requireEVMBalance(1300) })
	s.restartRelayerOn(ctx, h.agent, h.deployer, h.archive, validatorNode, r.initialHeight, relayerExportDB)

	// The validator reads the new chain with its old database and checkpoints.
	s.Require().NoError(h.validator.Start(ctx), "failed to restart the validator")
	s.waitForValidatorConsistency(ctx, validatorNode, h.validator)
	s.Run("new message signed and delivered", func() {
		sendToEVM(200)
		requireEVMBalance(1500)
		s.waitForCheckpoint(ctx, h.deployer, 2)
		s.Require().False(s.reorgFlagged(ctx, h.deployer), "validator reported a reorg")
	})
}

// TestHyperlaneMultisigExportBelowTip restarts Celestia from an export below
// the last height, with a message in the dropped blocks that the validator
// signed at merkle tree index 1. The new chain puts its next message at index
// 1 too. The validator starts again with an empty database indexed from the
// new chain, after its checkpoints from index 1 on are removed, and before the
// new messages are sent: it signs only leaves added after it starts. It then
// signs a different message at index 1, a conflicting checkpoint.
func (s *HyperlaneTestSuite) TestHyperlaneMultisigExportBelowTip() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping hyperlane multisig export below tip test in short mode")
	}
	ctx := context.Background()

	h := s.setupHyperlaneExportWith(ctx, hypValidatorKey)
	rethRecipient := ethcommon.HexToAddress("0xaF9053bB6c4346381C77C2FeD279B17ABAfCDf4d")
	sendToEVM := func(amount int64) { s.sendToEVMViaMerkleTree(ctx, h, rethRecipient, amount) }
	requireEVMBalance := func(want int64) {
		s.AssertERC20Balance(ctx, h.reth, h.tokenRouter, rethRecipient, big.NewInt(want))
	}

	sendToEVM(1000)
	requireEVMBalance(1000)
	exportAt, err := h.chain.Validators[0].Height(ctx)
	s.Require().NoError(err)

	// Above E: index 1, signed and delivered before the halt.
	sendToEVM(300)
	requireEVMBalance(1300)
	oldID := s.checkpointMessageID(ctx, h.deployer, 1)
	s.Require().NoError(h.agent.Stop(ctx), "failed to stop the relayer")
	s.Require().NoError(h.validator.RemoveContainer(ctx, tastoratypes.WithPreserveVolumes()), "failed to stop the validator")

	r := &exportRestart{exportAt: exportAt, archive: h.archive}
	s.exportAndRestart(ctx, h.chain, r)
	t.Logf("exported at height %d, new chain starts at %d", r.exportHeight, r.initialHeight)
	requireEVMBalance(1300)

	// A fresh relayer database indexed from the new chain, as for a noop ISM.
	validatorNode := h.chain.Validators[0]
	const recoveryDB = "relayer-db-recovery"
	s.restartRelayerOn(ctx, h.agent, h.deployer, validatorNode, validatorNode, r.initialHeight, recoveryDB)

	// The validator: empty database, no checkpoints from index 1 on, and the
	// latest index back to 0. The relayer config it reads now indexes
	// Celestia from the new chain's first height.
	_, stderr, err := h.deployer.Exec(ctx, s.logger, []string{"sh", "-c", fmt.Sprintf(
		// The agent parses index.json without trimming, so no newline.
		"rm -rf %s && rm -f %s/1_with_id.json %s/reorg_flag.json && printf 0 > %s/index.json",
		hyperlane.ValidatorDBDir, hyperlane.CheckpointDir, hyperlane.CheckpointDir, hyperlane.CheckpointDir)}, nil)
	s.Require().NoError(err, "failed to reset the validator: %s", stderr)
	s.Require().NoError(h.validator.Start(ctx), "failed to restart the validator")
	// It signs only leaves added after it has read the current tree.
	s.waitForValidatorConsistency(ctx, validatorNode, h.validator)

	sendToEVM(200) // the replacement, on index 1
	sendToEVM(50)  // the next message, on index 2
	s.waitForStoredMessages(ctx, validatorNode, h.agent, 2)
	s.restartRelayerOn(ctx, h.agent, h.deployer, validatorNode, validatorNode, r.initialHeight, recoveryDB)
	s.Run("replacement signed and delivered", func() { requireEVMBalance(1550) })
	s.Run("conflicting checkpoint at the reused index", func() {
		newID := s.checkpointMessageID(ctx, h.deployer, 1)
		s.Require().NotEqual(oldID, newID, "index 1 was not signed again for the replacement")
		t.Logf("index 1 signed for %s before the halt and for %s after it", oldID, newID)
	})
}

// sendToEVMViaMerkleTree sends a warp transfer to the EVM chain through the
// merkle tree hook, so the message enters the tree the validator signs; the
// mailbox's own hooks are noop.
func (s *HyperlaneTestSuite) sendToEVMViaMerkleTree(ctx context.Context, h *hyperlaneExportSetup, recipient ethcommon.Address, amount int64) {
	signer := h.chain.GetFaucetWallet()
	msg := &warptypes.MsgRemoteTransfer{
		Sender:            signer.GetFormattedAddress(),
		TokenId:           h.cfg.TokenID,
		DestinationDomain: h.rethDomain,
		Recipient:         evm.PadAddress(recipient),
		Amount:            sdkmath.NewInt(amount),
		CustomHookId:      &h.cfg.MerkleTreeHookID,
	}
	resp, err := cosmos.NewBroadcaster(h.chain).BroadcastMessages(ctx, signer, msg)
	s.Require().NoError(err)
	s.Require().Equalf(uint32(0), resp.Code, "transfer failed: %s", resp.RawLog)
}

// startHyperlaneValidator starts a validator agent for Celestia and waits until
// it has announced its checkpoint directory, which it does from the faucet
// account.
func (s *HyperlaneTestSuite) startHyperlaneValidator(ctx context.Context, chain *cosmos.Chain, deployer *hyperlane.Deployer, key string) *hyperlane.Agent {
	s.setCelestiaGasMultiplier(ctx, deployer, 2)
	s.Require().NoError(deployer.WriteValidatorConfig(ctx, hyperlane.ValidatorConfig{OriginChainName: HypCelestiaChainName, Key: key, Interval: 1}))
	agent, err := hyperlane.NewAgent(ctx, hyperlane.Config{
		Logger:          s.logger,
		DockerClient:    s.client,
		DockerNetworkID: s.network,
		HyperlaneImage:  container.NewImage("gcr.io/abacus-labs-dev/hyperlane-agent", "agents-v1.7.0", "1000:1000"),
	}, s.T().Name(), hyperlane.AgentTypeValidator, deployer)
	s.Require().NoError(err)
	s.Require().NoError(agent.Start(ctx), "failed to start the validator")
	t := s.T()
	t.Cleanup(func() {
		// The suite cleanup prints container logs only after this removes the
		// validator, so print them here.
		if t.Failed() {
			t.Logf("validator logs:\n%s", s.containerLogs(context.Background(), agent.ContainerLifecycle.ContainerName()))
		}
		_ = agent.Stop(ctx)
		_ = agent.Remove(ctx)
	})

	addr, err := hyperlane.ValidatorAddress(key)
	s.Require().NoError(err)
	client := ismtypes.NewQueryClient(chain.GetNode().GrpcConn)
	err = internal.WaitFor(ctx, 2*time.Minute, 2*time.Second, func(ctx context.Context) (bool, error) {
		res, err := client.AnnouncedStorageLocations(ctx, &ismtypes.QueryAnnouncedStorageLocationsRequest{
			MailboxId:        "0x68797065726c616e650000000000000000000000000000000000000000000000",
			ValidatorAddress: addr.Hex(),
		})
		return err == nil && len(res.StorageLocations) > 0, nil
	})
	s.Require().NoError(err, "validator %s did not announce its checkpoints", addr.Hex())
	// The merkle tree is empty here, and the validator reads it only after the
	// first message, so there is no consistency to wait for. It still signs the
	// first message: its database indexes Celestia from the first block.
	return agent
}

// waitForValidatorConsistency waits until the validator has read the current
// merkle tree of Celestia; from then on it signs every new leaf. node only runs
// curl against the agent's metrics.
func (s *HyperlaneTestSuite) waitForValidatorConsistency(ctx context.Context, node *cosmos.ChainNode, agent *hyperlane.Agent) {
	url := fmt.Sprintf("http://%s:%d/metrics", agent.Name(), relayerMetricsPort)
	err := internal.WaitFor(ctx, 2*time.Minute, 2*time.Second, func(ctx context.Context) (bool, error) {
		out, _, err := node.Exec(ctx, []string{"curl", "-sf", url}, nil)
		if err != nil {
			return false, nil
		}
		for line := range strings.SplitSeq(string(out), "\n") {
			if strings.HasPrefix(line, "hyperlane_reached_initial_consistency{") && strings.Contains(line, `chain="`+HypCelestiaChainName+`"`) {
				return strings.HasSuffix(strings.TrimSpace(line), " 1"), nil
			}
		}
		return false, nil
	})
	s.Require().NoError(err, "validator did not read the Celestia merkle tree")
}

// signedCheckpoint is a checkpoint file a validator agent writes.
type signedCheckpoint struct {
	Value struct {
		Checkpoint struct {
			Index uint32 `json:"index"`
		} `json:"checkpoint"`
		MessageID string `json:"message_id"`
	} `json:"value"`
	Signature json.RawMessage `json:"signature"`
}

// waitForCheckpoint waits until the validator has written its complete signed
// checkpoint for merkle tree index i, and returns its message id.
func (s *HyperlaneTestSuite) waitForCheckpoint(ctx context.Context, deployer *hyperlane.Deployer, i uint32) string {
	var id string
	err := internal.WaitFor(ctx, 2*time.Minute, 2*time.Second, func(ctx context.Context) (bool, error) {
		bz, err := deployer.ReadFile(ctx, hyperlane.CheckpointFile(i))
		if err != nil {
			return false, nil
		}
		var c signedCheckpoint
		if json.Unmarshal(bz, &c) != nil || c.Value.Checkpoint.Index != i || c.Value.MessageID == "" || len(c.Signature) == 0 {
			return false, nil
		}
		id = c.Value.MessageID
		return true, nil
	})
	s.Require().NoError(err, "validator did not sign index %d", i)
	return id
}

// checkpointMessageID returns the message id in the validator's signed
// checkpoint at merkle tree index i.
func (s *HyperlaneTestSuite) checkpointMessageID(ctx context.Context, deployer *hyperlane.Deployer, i uint32) string {
	return s.waitForCheckpoint(ctx, deployer, i)
}

// reorgFlagged reports whether the validator wrote a reorg flag next to its
// checkpoints.
func (s *HyperlaneTestSuite) reorgFlagged(ctx context.Context, deployer *hyperlane.Deployer) bool {
	_, err := deployer.ReadFile(ctx, "checkpoints/reorg_flag.json")
	return err == nil
}

// setCelestiaGasMultiplier sets the factor the agents apply to the gas of a
// Celestia transaction simulated with no fee. The simulation skips the fee
// deduction, so the default 1.35 leaves the validator's announce transaction
// out of gas.
func (s *HyperlaneTestSuite) setCelestiaGasMultiplier(ctx context.Context, deployer *hyperlane.Deployer, multiplier float64) {
	bz, err := deployer.ReadFile(ctx, "relayer-config.json")
	s.Require().NoError(err, "failed to read the relayer config")
	var relayerCfg map[string]any
	s.Require().NoError(json.Unmarshal(bz, &relayerCfg))
	chains, ok := relayerCfg["chains"].(map[string]any)
	s.Require().True(ok, "relayer config has no chains")
	celestiaCfg, ok := chains[HypCelestiaChainName].(map[string]any)
	s.Require().True(ok, "relayer config has no %s chain", HypCelestiaChainName)
	celestiaCfg["gasMultiplier"] = multiplier
	bz, err = json.Marshal(relayerCfg)
	s.Require().NoError(err)
	s.Require().NoError(deployer.WriteFile(ctx, "relayer-config.json", bz), "failed to write the relayer config")
}
