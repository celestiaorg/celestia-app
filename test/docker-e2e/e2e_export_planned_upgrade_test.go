package docker_e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	circuittypes "cosmossdk.io/x/circuit/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v12/pkg/user"
	"github.com/celestiaorg/celestia-app/v12/test/util/genesis"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	cometcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v8/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"
	mobyclient "github.com/moby/moby/client"

	"celestiaorg/celestia-app/test/docker-e2e/internal"
)

const (
	// plannedUpgradeName is the name of the upgrade plan of the planned restart.
	plannedUpgradeName = "planned-restart"
	// plannedUpgradeDelay is how many blocks after the proposal the chain
	// halts. At one to two blocks a second it covers the voting period, the
	// 30 s Hermes stop and the pending transfer.
	plannedUpgradeDelay = 200
	// plannedUpgradeTimeout bounds the waits for the halt and the archives.
	plannedUpgradeTimeout = 5 * time.Minute
)

// TestIBCPlannedUpgradeRestart restarts Celestia from an export as a new
// revision through a planned IBC upgrade. One governance proposal opens the
// circuit breaker for MsgIBCSoftwareUpgrade, schedules the upgrade at H and
// closes the circuit again. The chain halts at H and restarts as revision 1
// from a plain export, at H-1, which the app allows because the EndBlock of
// H-1 changed no validators. Hermes upgrades simapp's client of Celestia from
// two archives of the old chain that skip the upgrade.
func (s *IBCTestSuite) TestIBCPlannedUpgradeRestart() {
	if testing.Short() {
		s.T().Skip("skipping IBC planned upgrade restart test in short mode")
	}
	ctx := context.Background()

	s.T().Cleanup(func() { s.celestiaModifiers = nil })
	s.celestiaModifiers = []genesis.Modifier{fastGov(exportCodec())}
	// Another test may leave a short trusting period that expires before H.
	s.hermesOpts = nil
	s.setupIBCInfrastructure(appconsts.Version, true)
	archives := []*cosmos.ChainNode{s.addArchiveNode(ctx, s.chainA), s.addArchiveNode(ctx, s.chainA)}
	s.Require().NoError(s.hermes.Start(ctx), "failed to start hermes")
	s.testTokenTransfers(ctx, s.transferChannel)
	s.testReverseTokenTransfers(ctx, s.transferChannel)

	upgradeType := sdk.MsgTypeURL(&clienttypes.MsgIBCSoftwareUpgrade{})
	s.Require().Contains(s.disabledMsgs(ctx), upgradeType, "MsgIBCSoftwareUpgrade is not disabled")
	oldChainID := s.chainA.GetChainID()
	newChainID := oldChainID + "-1"
	height, err := s.chainA.Validators[0].Height(ctx)
	s.Require().NoError(err)
	upgradeHeight := height + plannedUpgradeDelay
	initialHeight := upgradeHeight + 1 + restartHeightGap
	newLatest := clienttypes.NewHeight(1, uint64(initialHeight))
	clientID := s.celestiaClientID(ctx)
	s.scheduleIBCUpgrade(ctx, clientID, upgradeHeight, newChainID, newLatest)

	conn := s.chainA.GetNode().GrpcConn
	plan, err := upgradetypes.NewQueryClient(conn).CurrentPlan(ctx, &upgradetypes.QueryCurrentPlanRequest{})
	s.Require().NoError(err, "failed to query the upgrade plan")
	s.Require().NotNil(plan.Plan, "no upgrade plan")
	s.Require().Equal(upgradeHeight, plan.Plan.Height, "unexpected upgrade height")
	upgraded, err := clienttypes.NewQueryClient(conn).UpgradedClientState(ctx, &clienttypes.QueryUpgradedClientStateRequest{})
	s.Require().NoError(err, "failed to query the upgraded client state")
	s.Require().NotNil(upgraded.UpgradedClientState, "no upgraded client state")
	s.Require().Contains(s.disabledMsgs(ctx), upgradeType, "the proposal left MsgIBCSoftwareUpgrade enabled")

	// Hermes.Stop deletes the config volume, so stop the container only.
	s.Require().NoError(s.hermes.StopContainer(ctx), "failed to stop hermes")
	pending := sdkmath.NewInt(100000)
	voucher := s.calculateIBCDenom(s.transferChannel.CounterpartyPort, s.transferChannel.CounterpartyID, "utia")
	receiver := s.chainB.GetFaucetWallet().GetFormattedAddress()
	voucherBefore := s.getBalance(ctx, s.chainB, receiver, voucher)
	seq := s.nextSequenceSend(ctx, s.chainA, s.transferChannel.PortID, s.transferChannel.ChannelID)
	resp := s.sendTransfer(ctx, pending, time.Now().Add(time.Hour))
	s.Require().LessOrEqual(resp.Height, upgradeHeight-1, "the pending transfer is above the export height")
	before := s.ibcBalances(ctx, "")

	seed := s.waitForUpgradeHalt(ctx)
	s.Require().NoError(s.chainA.Stop(ctx), "failed to stop the chain")
	s.seedArchives(ctx, seed, archives, upgradeHeight)

	r := &exportRestart{newChainID: newChainID, initialHeight: initialHeight}
	s.exportAndRestart(ctx, s.chainA, r)
	s.Require().Equal(upgradeHeight-1, r.exportHeight, "the plain export is not at H-1")
	s.celestiaTxClient, err = s.setupTxClient(ctx, s.chainA)
	s.Require().NoError(err, "failed to set up the tx client on the new chain")
	s.Require().Equal(before, s.ibcBalances(ctx, ""), "balances differ from the export height")
	// The upgraded client trusts the next validators of header H-1, the
	// validators of block H; the export at H-1 holds them only if the EndBlock
	// of H-1 changed no validators, so header H has equal hashes. The app
	// allows the export at H-1 only then. Nothing in this test changes
	// validators.
	archive := archives[0]
	header := s.block(ctx, archive, upgradeHeight).Block.Header
	s.Require().Equal(header.ValidatorsHash, header.NextValidatorsHash, "the EndBlock of H-1 changed validators")
	s.Require().Contains(s.disabledMsgs(ctx), upgradeType, "the new chain enabled MsgIBCSoftwareUpgrade")

	// Hermes builds the header at H, so it needs the commit of H, the app
	// state after H and a node that is not catching up.
	s.waitUntil(ctx, plannedUpgradeTimeout, "archive does not serve the upgrade height", func(ctx context.Context) bool {
		if _, err := archive.Client.Commit(ctx, &upgradeHeight); err != nil {
			return false
		}
		info, err := archive.Client.ABCIInfo(ctx)
		if err != nil || info.Response.LastBlockHeight < upgradeHeight {
			return false
		}
		status, err := archive.Client.Status(ctx)
		return err == nil && !status.SyncInfo.CatchingUp
	})
	validatorHost := s.moveHermesHost(ctx, oldChainID, archive.HostName())
	out := s.hermesExec(ctx, "upgrade", "client", "--host-chain", simappChainID, "--client", clientID,
		"--upgrade-height", strconv.FormatInt(upgradeHeight, 10))
	s.T().Logf("hermes upgrade client at height %d: %s", upgradeHeight, out)
	cs := s.celestiaClientState(ctx, clientID)
	s.Require().Equal(newChainID, cs.ChainId, "client did not move to the new chain-id")
	s.Require().Equal(newLatest, cs.LatestHeight, "unexpected client height after the upgrade")
	s.requireCelestiaClientActive(ctx)

	s.moveHermesHost(ctx, oldChainID, validatorHost)
	s.moveHermesToChainID(ctx, oldChainID, newChainID)
	s.Require().NoError(s.hermes.StartContainer(ctx), "failed to restart hermes")
	s.Run("pending transfer delivered once", func() {
		ch := s.transferChannel
		s.waitUntil(ctx, 90*time.Second, "pending transfer was not acknowledged after the restart", func(ctx context.Context) bool {
			return !s.packetCommitted(ctx, s.chainA, ch.PortID, ch.ChannelID, seq)
		})
		s.Require().Equal(voucherBefore.Add(pending), s.getBalance(ctx, s.chainB, receiver, voucher), "pending transfer was not delivered once")
		// A refund would release escrow and pay the sender back.
		s.Require().Equal(before, s.ibcBalances(ctx, ""), "pending transfer was delivered and refunded")
	})
	s.Run("simapp to celestia", func() { s.testReverseTokenTransfers(ctx, s.transferChannel) })
	s.Run("celestia to simapp", func() { s.testTokenTransfers(ctx, s.transferChannel) })
	s.Run("client active at revision 1", func() {
		s.requireCelestiaClientActive(ctx)
		s.Require().Equal(uint64(1), s.celestiaClientState(ctx, clientID).LatestHeight.RevisionNumber)
	})
}

// fastGov shortens Celestia's governance so a proposal passes within the
// test.
func fastGov(cdc codec.Codec) genesis.Modifier {
	return func(state map[string]json.RawMessage) map[string]json.RawMessage {
		var gs govv1.GenesisState
		cdc.MustUnmarshalJSON(state[govtypes.ModuleName], &gs)
		voting, expedited := 20*time.Second, 10*time.Second
		gs.Params.VotingPeriod = &voting
		gs.Params.ExpeditedVotingPeriod = &expedited
		gs.Params.MinDeposit = sdk.NewCoins(sdk.NewInt64Coin("utia", 1))
		gs.Params.ExpeditedMinDeposit = sdk.NewCoins(sdk.NewInt64Coin("utia", 2))
		gs.Params.Quorum = "0.000001"
		state[govtypes.ModuleName] = cdc.MustMarshalJSON(&gs)
		return state
	}
}

// scheduleIBCUpgrade passes a Celestia governance proposal that enables
// MsgIBCSoftwareUpgrade, schedules an upgrade at height to a client of
// newChainID at latest, and disables the message again.
func (s *IBCTestSuite) scheduleIBCUpgrade(ctx context.Context, clientID string, height int64, newChainID string, latest clienttypes.Height) {
	upgradedClient := *s.celestiaClientState(ctx, clientID)
	upgradedClient.ChainId = newChainID
	upgradedClient.LatestHeight = latest

	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	typeURLs := []string{sdk.MsgTypeURL(&clienttypes.MsgIBCSoftwareUpgrade{})}
	upgrade, err := clienttypes.NewMsgIBCSoftwareUpgrade(authority, upgradetypes.Plan{Name: plannedUpgradeName, Height: height}, &upgradedClient)
	s.Require().NoError(err)
	msgs := []sdk.Msg{
		&circuittypes.MsgResetCircuitBreaker{Authority: authority, MsgTypeUrls: typeURLs},
		upgrade,
		&circuittypes.MsgTripCircuitBreaker{Authority: authority, MsgTypeUrls: typeURLs},
	}
	proposer := s.celestiaTxClient.DefaultAddress()
	deposit := sdk.NewCoins(sdk.NewInt64Coin("utia", 1))
	submit, err := govv1.NewMsgSubmitProposal(msgs, deposit, proposer.String(), "", "Planned restart", "Upgrade IBC clients of Celestia to a new revision.", false)
	s.Require().NoError(err)
	_, err = s.celestiaTxClient.SubmitTx(ctx, []sdk.Msg{submit}, user.SetGasLimit(500000), user.SetFee(12500))
	s.Require().NoError(err, "failed to submit the upgrade proposal")

	gov := govv1.NewQueryClient(s.chainA.GetNode().GrpcConn)
	proposals, err := gov.Proposals(ctx, &govv1.QueryProposalsRequest{})
	s.Require().NoError(err)
	s.Require().NotEmpty(proposals.Proposals)
	id := proposals.Proposals[len(proposals.Proposals)-1].Id
	// The proposer is the faucet, a validator, so its vote alone passes.
	vote := govv1.NewMsgVote(proposer, id, govv1.OptionYes, "")
	_, err = s.celestiaTxClient.SubmitTx(ctx, []sdk.Msg{vote}, user.SetGasLimit(200000), user.SetFee(5000))
	s.Require().NoError(err, "failed to vote on the upgrade proposal")

	err = internal.WaitFor(ctx, 2*time.Minute, 2*time.Second, func(ctx context.Context) (bool, error) {
		p, err := gov.Proposal(ctx, &govv1.QueryProposalRequest{ProposalId: id})
		if err != nil {
			return false, nil
		}
		if p.Proposal.Status == govv1.StatusRejected || p.Proposal.Status == govv1.StatusFailed {
			return false, fmt.Errorf("proposal %d ended as %s: %s", id, p.Proposal.Status, p.Proposal.FailedReason)
		}
		return p.Proposal.Status == govv1.StatusPassed, nil
	})
	s.Require().NoError(err, "upgrade proposal did not pass")
}

// disabledMsgs returns the message types disabled by Celestia's circuit breaker.
func (s *IBCTestSuite) disabledMsgs(ctx context.Context) []string {
	res, err := circuittypes.NewQueryClient(s.chainA.GetNode().GrpcConn).DisabledList(ctx, &circuittypes.QueryDisabledListRequest{})
	s.Require().NoError(err, "failed to query the circuit breaker")
	return res.DisabledList
}

// celestiaClientState returns simapp's client of Celestia.
func (s *IBCTestSuite) celestiaClientState(ctx context.Context, clientID string) *ibctm.ClientState {
	res, err := clienttypes.NewQueryClient(s.chainB.GetNode().GrpcConn).ClientState(ctx, &clienttypes.QueryClientStateRequest{ClientId: clientID})
	s.Require().NoError(err, "failed to query client %s", clientID)
	var cs exported.ClientState
	s.Require().NoError(exportCodec().UnpackAny(res.ClientState, &cs))
	tm, ok := cs.(*ibctm.ClientState)
	s.Require().True(ok, "client %s is not a tendermint client", clientID)
	return tm
}

// waitForUpgradeHalt waits until the validators stop at the upgrade height and
// returns one that exited there, so its block store has the upgrade block.
// The first validator to commit that block can exit before its precommit
// reaches the others, which then wait one height below, so once one exits the
// rest get haltStallTimeout to follow.
func (s *IBCTestSuite) waitForUpgradeHalt(ctx context.Context) *cosmos.ChainNode {
	exited := func(ctx context.Context) ([]*cosmos.ChainNode, error) {
		var nodes []*cosmos.ChainNode
		for _, v := range s.chainA.Validators {
			inspect, err := s.client.ContainerInspect(ctx, v.ContainerLifecycle.ContainerName(), mobyclient.ContainerInspectOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to inspect %s: %w", v.Name(), err)
			}
			if !inspect.Container.State.Running {
				nodes = append(nodes, v)
			}
		}
		return nodes, nil
	}
	// An inspect cut off by a wait's deadline only ends that wait.
	s.waitUntil(ctx, plannedUpgradeTimeout, "no validator halted at the upgrade height", func(ctx context.Context) bool {
		nodes, err := exited(ctx)
		return err == nil && len(nodes) > 0
	})
	// Stalled validators keep running, so this wait may time out.
	_ = internal.WaitFor(ctx, haltStallTimeout, time.Second, func(ctx context.Context) (bool, error) {
		nodes, err := exited(ctx)
		return err == nil && len(nodes) == len(s.chainA.Validators), nil
	})
	halted, err := exited(ctx)
	s.Require().NoError(err)
	s.Require().NotEmpty(halted, "no validator halted at the upgrade height")
	if len(halted) < len(s.chainA.Validators) {
		s.T().Logf("%d of %d validators exited at the upgrade height; the rest wait one height below", len(halted), len(s.chainA.Validators))
	}
	return halted[0]
}

// seedArchives copies the data of a halted validator, which holds the upgrade
// block and its commit, to the stopped archives. Gossip does not guarantee
// that an archive got that block. The archives keep their own config and sign
// state, dial only each other and skip the upgrade, so they execute the
// upgrade block on start and serve it to Hermes.
func (s *IBCTestSuite) seedArchives(ctx context.Context, from *cosmos.ChainNode, archives []*cosmos.ChainNode, upgradeHeight int64) {
	_, stderr, err := from.Exec(ctx, []string{"tar", "-cf", from.HomeDir() + "/seed.tar", "-C", from.HomeDir(), "data"}, nil)
	s.Require().NoError(err, "failed to pack the data of %s: %s", from.Name(), stderr)
	data, err := from.ReadFile(ctx, "seed.tar")
	s.Require().NoError(err, "failed to read the data of %s", from.Name())
	for i, a := range archives {
		s.Require().NoError(a.WriteFile(ctx, "seed.tar", data), "failed to write the data to %s", a.Name())
		// The validator's consensus WAL holds its own votes; leave it out.
		unpack := "cd " + a.HomeDir() + " && mv data/priv_validator_state.json pv_state.json && rm -rf data && tar -xf seed.tar" +
			" && rm -rf seed.tar data/cs.wal && mv pv_state.json data/priv_validator_state.json"
		_, stderr, err := a.Exec(ctx, []string{"sh", "-c", unpack}, nil)
		s.Require().NoError(err, "failed to unpack the data on %s: %s", a.Name(), stderr)

		other := archives[(i+1)%len(archives)]
		id, err := other.NodeID(ctx)
		s.Require().NoError(err)
		err = config.Modify(ctx, a, "config/config.toml", func(cfg *cometcfg.Config) {
			cfg.P2P.PersistentPeers = fmt.Sprintf("%s@%s:26656", id, other.HostName())
			cfg.P2P.Seeds = ""
			cfg.P2P.PexReactor = false
		})
		s.Require().NoError(err, "failed to peer %s", a.Name())
		a.AdditionalStartArgs = append(slices.Clip(a.AdditionalStartArgs), "--unsafe-skip-upgrades="+strconv.FormatInt(upgradeHeight, 10))
	}
}

// moveHermesHost points the RPC, gRPC and event source of the Hermes entry of
// chainID at host and returns the host it used before.
func (s *IBCTestSuite) moveHermesHost(ctx context.Context, chainID, host string) string {
	cfg, err := s.hermes.GetConfig(ctx)
	s.Require().NoError(err, "failed to read the hermes config")
	for i := range cfg.Chains {
		c := &cfg.Chains[i]
		if c.ID != chainID {
			continue
		}
		rpc, err := url.Parse(c.RPCAddr)
		s.Require().NoError(err)
		old := rpc.Hostname()
		c.RPCAddr = strings.ReplaceAll(c.RPCAddr, old, host)
		c.GRPCAddr = strings.ReplaceAll(c.GRPCAddr, old, host)
		c.EventSource["url"] = strings.ReplaceAll(fmt.Sprint(c.EventSource["url"]), old, host)
		bz, err := cfg.ToTOML()
		s.Require().NoError(err)
		s.Require().NoError(s.hermes.WriteFile(ctx, ".hermes/config.toml", bz), "failed to write the hermes config")
		return old
	}
	s.Require().FailNow("hermes has no chain " + chainID)
	return ""
}
