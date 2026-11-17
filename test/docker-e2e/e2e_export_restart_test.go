package docker_e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	hypcoretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/docker/hyperlane"
	"github.com/celestiaorg/tastora/framework/docker/ibc/relayer"
	"github.com/celestiaorg/tastora/framework/testutil/query"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v8/modules/core/03-connection/types"
	"github.com/cosmos/ibc-go/v8/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"
	ethcommon "github.com/ethereum/go-ethereum/common"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"celestiaorg/celestia-app/test/docker-e2e/internal"
)

// TestIBCExportRestart checks that IBC transfers, a pending packet and an ICA
// survive a halt, an export and a restart of Celestia from that export with
// the same chain-id. Hermes moves simapp's client of Celestia across the
// restart with the old blocks of an archive node.
func (s *IBCTestSuite) TestIBCExportRestart() {
	s.runIBCExportRestart(false)
}

// TestIBCExportRestartNewChainID is TestIBCExportRestart with a new chain-id.
// simapp's client of Celestia cannot follow a new chain-id, so it is left to
// expire and recovered by a governance proposal with MsgRecoverClient.
func (s *IBCTestSuite) TestIBCExportRestartNewChainID() {
	s.runIBCExportRestart(true)
}

// recoverTrustingPeriod is the trusting period of simapp's client of Celestia
// in the new chain-id test, short so the client expires within the test.
const recoverTrustingPeriod = "90s"

func (s *IBCTestSuite) runIBCExportRestart(newChainID bool) {
	if testing.Short() {
		s.T().Skip("skipping IBC export restart test in short mode")
	}
	ctx := context.Background()

	if newChainID {
		s.hermesOpts = []relayer.ConfigOption{func(cfg *relayer.HermesConfig) {
			for i := range cfg.Chains {
				if cfg.Chains[i].ID != simappChainID {
					cfg.Chains[i].TrustingPeriod = recoverTrustingPeriod
				}
			}
		}}
	}
	s.setupIBCInfrastructure(appconsts.Version, true)
	archive := s.addArchiveNode(ctx, s.chainA)
	s.Require().NoError(s.hermes.Start(ctx), "failed to start hermes")
	s.testTokenTransfers(ctx, s.transferChannel)
	s.testReverseTokenTransfers(ctx, s.transferChannel)
	s.registerInterchainAccount(ctx)
	icaAddress := s.verifyICARegistration(ctx)
	s.fundICAAccount(ctx, icaAddress)

	// Hermes.Stop deletes the config volume, so stop the container only.
	s.Require().NoError(s.hermes.StopContainer(ctx), "failed to stop hermes")
	pending := sdkmath.NewInt(100000)
	voucher := s.calculateIBCDenom(s.transferChannel.CounterpartyPort, s.transferChannel.CounterpartyID, "utia")
	receiver := s.chainB.GetFaucetWallet().GetFormattedAddress()
	voucherBefore := s.getBalance(ctx, s.chainB, receiver, voucher)
	s.submitTransfer(ctx, pending)
	before := s.ibcBalances(ctx, icaAddress)

	oldChainID := s.chainA.GetChainID()
	r := &exportRestart{archive: archive}
	if newChainID {
		r.newChainID = oldChainID + "-restart"
	}
	s.exportAndRestart(ctx, s.chainA, r)
	s.T().Logf("exported at height %d, new chain %s starts at %d", r.exportHeight, s.chainA.GetChainID(), r.initialHeight)
	var err error
	s.celestiaTxClient, err = s.setupTxClient(ctx, s.chainA)
	s.Require().NoError(err, "failed to set up the tx client on the new chain")
	s.Require().Equal(before, s.ibcBalances(ctx, icaAddress), "balances changed across the restart")

	clientID := s.celestiaClientID(ctx)
	if newChainID {
		s.moveHermesToChainID(ctx, oldChainID, r.newChainID)
		s.waitForClientStatus(ctx, clientID, exported.Expired, 5*time.Minute)
		substitute := s.createSubstituteClient(ctx, r.newChainID)
		s.recoverClientByGovernance(ctx, clientID, substitute)
	} else {
		s.updateClientAcrossRestart(ctx, clientID, archive)
	}
	s.requireCelestiaClientActive(ctx)

	s.Require().NoError(s.hermes.StartContainer(ctx), "failed to restart hermes")
	s.Run("pending transfer delivered once", func() {
		want := voucherBefore.Add(pending)
		err := internal.WaitFor(ctx, 90*time.Second, time.Second, func(ctx context.Context) (bool, error) {
			return s.getBalance(context.Background(), s.chainB, receiver, voucher).Equal(want), nil
		})
		s.Require().NoError(err, "pending transfer was not delivered after the restart")
		// A refund would release escrow and pay the sender back.
		s.Require().Equal(before, s.ibcBalances(ctx, icaAddress), "pending transfer was delivered and refunded")
	})
	s.Run("simapp to celestia", func() { s.testReverseTokenTransfers(ctx, s.transferChannel) })
	s.Run("ica bank send", func() { s.performICABankSend(ctx, icaAddress) })
	s.Run("celestia to simapp", func() { s.testTokenTransfers(ctx, s.transferChannel) })
	s.Run("client active", func() { s.requireCelestiaClientActive(ctx) })
}

// simappChainID is the chain-id of the simapp counterparty.
const simappChainID = "chain-b"

// celestiaClientID returns the id of simapp's client of Celestia.
func (s *IBCTestSuite) celestiaClientID(ctx context.Context) string {
	conn := s.chainB.GetNode().GrpcConn
	connection, err := connectiontypes.NewQueryClient(conn).Connection(ctx, &connectiontypes.QueryConnectionRequest{ConnectionId: s.connection.CounterpartyID})
	s.Require().NoError(err, "failed to query the simapp connection")
	return connection.Connection.ClientId
}

// hermesExec runs a one-shot hermes command next to the stopped relayer.
func (s *IBCTestSuite) hermesExec(ctx context.Context, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	stdout, stderr, err := s.hermes.Exec(ctx, s.logger, append([]string{"hermes", "--json"}, args...), nil)
	s.Require().NoError(err, "hermes %v failed: %s%s", args, stdout, stderr)
	return string(stdout)
}

// updateClientAcrossRestart updates simapp's client of Celestia to the new
// chain. The client trusts an old height, so Hermes reads the light blocks at
// and below the restart height from the archive of the old chain.
func (s *IBCTestSuite) updateClientAcrossRestart(ctx context.Context, clientID string, archive *cosmos.ChainNode) {
	status, err := archive.Client.Status(ctx)
	s.Require().NoError(err, "failed to query the archive")
	restartHeight := status.SyncInfo.LatestBlockHeight
	out := s.hermesExec(ctx, "update", "client",
		"--host-chain", simappChainID,
		"--client", clientID,
		"--restart-height", strconv.FormatInt(restartHeight, 10),
		"--archive-address", "http://"+archive.HostName()+":26657",
	)
	s.T().Logf("hermes update client across the restart at height %d: %s", restartHeight, out)
}

// moveHermesToChainID points the Hermes config and key at the new chain-id.
// The relayer account is in the exported state, so the same key keeps working.
func (s *IBCTestSuite) moveHermesToChainID(ctx context.Context, oldChainID, newChainID string) {
	cfg, err := s.hermes.GetConfig(ctx)
	s.Require().NoError(err, "failed to read the hermes config")
	moved := false
	for i := range cfg.Chains {
		if cfg.Chains[i].ID == oldChainID {
			cfg.Chains[i].ID = newChainID
			moved = true
		}
	}
	s.Require().True(moved, "hermes has no chain %s", oldChainID)
	bz, err := cfg.ToTOML()
	s.Require().NoError(err)
	s.Require().NoError(s.hermes.WriteFile(ctx, ".hermes/config.toml", bz), "failed to write the hermes config")

	keys := s.hermes.HomeDir() + "/.hermes/keys/"
	_, stderr, err := s.hermes.Exec(ctx, s.logger, []string{"sh", "-c", fmt.Sprintf("cp -r %s%s %s%s", keys, oldChainID, keys, newChainID)}, nil)
	s.Require().NoError(err, "failed to copy the hermes key: %s", stderr)
}

// waitForClientStatus waits until simapp's client has the given status.
func (s *IBCTestSuite) waitForClientStatus(ctx context.Context, clientID string, want exported.Status, timeout time.Duration) {
	client := clienttypes.NewQueryClient(s.chainB.GetNode().GrpcConn)
	err := internal.WaitFor(ctx, timeout, 5*time.Second, func(ctx context.Context) (bool, error) {
		status, err := client.ClientStatus(ctx, &clienttypes.QueryClientStatusRequest{ClientId: clientID})
		if err != nil {
			return false, nil
		}
		return status.Status == want.String(), nil
	})
	s.Require().NoError(err, "client %s did not become %s", clientID, want)
}

// createSubstituteClient creates a client of the new Celestia chain on simapp
// and returns its id.
func (s *IBCTestSuite) createSubstituteClient(ctx context.Context, chainID string) string {
	s.hermesExec(ctx, "create", "client", "--host-chain", simappChainID, "--reference-chain", chainID)
	states, err := clienttypes.NewQueryClient(s.chainB.GetNode().GrpcConn).ClientStates(ctx, &clienttypes.QueryClientStatesRequest{})
	s.Require().NoError(err, "failed to query the simapp clients")
	cdc := exportCodec()
	for _, state := range states.ClientStates {
		var cs exported.ClientState
		if err := cdc.UnpackAny(state.ClientState, &cs); err != nil {
			continue
		}
		if tm, ok := cs.(*ibctm.ClientState); ok && tm.ChainId == chainID {
			return state.ClientId
		}
	}
	s.Require().FailNow("no client of %s on simapp", chainID)
	return ""
}

// recoverClientByGovernance points the expired client at the substitute with a
// MsgRecoverClient governance proposal on simapp that its validator votes for.
func (s *IBCTestSuite) recoverClientByGovernance(ctx context.Context, subject, substitute string) {
	conn := s.chainB.GetNode().GrpcConn
	proposer := s.chainB.GetFaucetWallet()
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	recover := clienttypes.NewMsgRecoverClient(authority, subject, substitute)
	deposit := sdk.NewCoins(sdk.NewInt64Coin(s.chainB.Config.Denom, 1))
	submit, err := govv1.NewMsgSubmitProposal([]sdk.Msg{recover}, deposit, proposer.GetFormattedAddress(), "", "Recover the Celestia client", "Celestia restarted from an export with a new chain-id.", false)
	s.Require().NoError(err)
	resp, err := s.chainB.BroadcastMessages(ctx, proposer, submit)
	s.Require().NoError(err, "failed to submit the recover proposal")
	s.Require().Equal(uint32(0), resp.Code, "recover proposal failed: %s", resp.RawLog)

	gov := govv1.NewQueryClient(conn)
	proposals, err := gov.Proposals(ctx, &govv1.QueryProposalsRequest{})
	s.Require().NoError(err)
	s.Require().NotEmpty(proposals.Proposals)
	id := proposals.Proposals[len(proposals.Proposals)-1].Id

	v := s.chainB.Validators[0]
	vote := []string{"simd", "tx", "gov", "vote", strconv.FormatUint(id, 10), "yes",
		"--from", "validator", "--keyring-backend", "test", "--home", v.HomeDir(),
		"--chain-id", s.chainB.GetChainID(), "--node", "tcp://" + v.HostName() + ":26657",
		"--fees", "2000" + s.chainB.Config.Denom, "--yes", "--output", "json"}
	stdout, stderr, err := v.Exec(ctx, vote, nil)
	s.Require().NoError(err, "vote failed: %s%s", stdout, stderr)

	err = internal.WaitFor(ctx, 2*time.Minute, 3*time.Second, func(ctx context.Context) (bool, error) {
		p, err := gov.Proposal(ctx, &govv1.QueryProposalRequest{ProposalId: id})
		if err != nil {
			return false, nil
		}
		if p.Proposal.Status == govv1.StatusRejected || p.Proposal.Status == govv1.StatusFailed {
			return false, fmt.Errorf("proposal %d ended as %s: %s", id, p.Proposal.Status, p.Proposal.FailedReason)
		}
		return p.Proposal.Status == govv1.StatusPassed, nil
	})
	s.Require().NoError(err, "recover proposal did not pass")
}

// submitTransfer sends utia from Celestia to simapp without waiting for delivery.
func (s *IBCTestSuite) submitTransfer(ctx context.Context, amount sdkmath.Int) {
	s.sendTransfer(ctx, amount, time.Now().Add(time.Hour))
}

// sendTransfer sends utia from Celestia to simapp with the given timeout
// without waiting for delivery.
func (s *IBCTestSuite) sendTransfer(ctx context.Context, amount sdkmath.Int, timeout time.Time) *user.TxResponse {
	msg := ibctransfertypes.NewMsgTransfer(
		s.transferChannel.PortID,
		s.transferChannel.ChannelID,
		sdk.NewCoin("utia", amount),
		s.chainA.GetFaucetWallet().GetFormattedAddress(),
		s.chainB.GetFaucetWallet().GetFormattedAddress(),
		clienttypes.ZeroHeight(),
		uint64(timeout.UnixNano()),
		"",
	)
	resp, err := s.celestiaTxClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(200000), user.SetFee(5000))
	s.Require().NoError(err, "failed to submit the transfer")
	s.Require().Equal(uint32(0), resp.Code, "transfer failed")
	return resp
}

// ibcBalances returns the Celestia balances an IBC transfer or an ICA can move.
// An empty icaAddress leaves out the ICA.
func (s *IBCTestSuite) ibcBalances(ctx context.Context, icaAddress string) map[string]string {
	conn := s.chainA.GetNode().GrpcConn
	faucet := s.chainA.GetFaucetWallet().GetFormattedAddress()
	escrow := ibctransfertypes.GetEscrowAddress(s.transferChannel.PortID, s.transferChannel.ChannelID).String()
	stakeVoucher := s.calculateIBCDenom(s.transferChannel.PortID, s.transferChannel.ChannelID, "stake")

	total, err := ibctransfertypes.NewQueryClient(conn).TotalEscrowForDenom(ctx, &ibctransfertypes.QueryTotalEscrowForDenomRequest{Denom: "utia"})
	s.Require().NoError(err, "failed to query total escrow")
	balances := map[string]string{
		"faucet utia":          s.getBalance(ctx, s.chainA, faucet, "utia").String(),
		"faucet stake voucher": s.getBalance(ctx, s.chainA, faucet, stakeVoucher).String(),
		"escrow utia":          s.getBalance(ctx, s.chainA, escrow, "utia").String(),
		"total escrow utia":    total.Amount.String(),
	}
	if icaAddress != "" {
		balances["ica utia"] = s.getBalance(ctx, s.chainA, icaAddress, "utia").String()
	}
	return balances
}

// requireCelestiaClientActive checks that simapp's client of Celestia is active.
func (s *IBCTestSuite) requireCelestiaClientActive(ctx context.Context) {
	conn := s.chainB.GetNode().GrpcConn
	connection, err := connectiontypes.NewQueryClient(conn).Connection(ctx, &connectiontypes.QueryConnectionRequest{ConnectionId: s.connection.CounterpartyID})
	s.Require().NoError(err, "failed to query the simapp connection")
	status, err := clienttypes.NewQueryClient(conn).ClientStatus(ctx, &clienttypes.QueryClientStatusRequest{ClientId: connection.Connection.ClientId})
	s.Require().NoError(err, "failed to query the client status")
	s.Require().Equal(exported.Active.String(), status.Status, "simapp's client of Celestia is not active")
}

// TestHyperlaneExportRestart checks that warp collateral, a pending message and
// replay protection survive a halt, an export and a restart of Celestia from that export.
func (s *HyperlaneTestSuite) TestHyperlaneExportRestart() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping hyperlane export restart test in short mode")
	}
	ctx := context.Background()

	h := s.setupHyperlaneExport(ctx)
	chain, archive, reth, hyp, cfg, tokenRouter, agent := h.chain, h.archive, h.reth, h.deployer, h.cfg, h.tokenRouter, h.agent
	rethDomain, celestiaDomain := h.rethDomain, h.celestiaDomain
	rethRecipient := ethcommon.HexToAddress("0xaF9053bB6c4346381C77C2FeD279B17ABAfCDf4d")
	celestiaRecipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
	celestiaRecipientBz, err := bech32ToBytes(celestiaRecipient)
	s.Require().NoError(err)

	s.SendTransferRemoteTx(ctx, chain, cfg.TokenID, rethDomain, rethRecipient, sdkmath.NewInt(1000))
	s.AssertERC20Balance(ctx, reth, tokenRouter, rethRecipient, big.NewInt(1000))
	// The EVM sender is rethRecipient, so this leaves it 500.
	received := sdkmath.NewInt(500)
	s.SendTransferRemoteTxEvm(ctx, reth, tokenRouter, celestiaDomain, celestiaRecipientBz, received)
	s.AssertBankBalance(ctx, chain, celestiaRecipient, chain.Config.Denom, received)
	erc20 := sdkmath.NewInt(500)
	delivered := s.processedMessage(ctx, chain)

	s.Require().NoError(agent.Stop(ctx), "failed to stop the relayer")
	pending := sdkmath.NewInt(300)
	s.SendTransferRemoteTx(ctx, chain, cfg.TokenID, rethDomain, rethRecipient, pending)
	before := s.hyperlaneState(ctx, chain, cfg, celestiaRecipient, delivered.MessageId)

	// The bridge node is not re-initialized for the new chain, whose first
	// height leaves a gap; DA recovery is a separate procedure. If the EVM chain
	// stops without DA, the relayer checks below time out.
	r := &exportRestart{archive: archive}
	s.exportAndRestart(ctx, chain, r)
	t.Logf("exported at height %d, new chain starts at %d", r.exportHeight, r.initialHeight)
	s.Require().Equal(before, s.hyperlaneState(ctx, chain, cfg, celestiaRecipient, delivered.MessageId), "hyperlane state changed across the restart")
	s.Require().Equal(erc20.BigInt(), s.QueryERC20Balance(ctx, reth, tokenRouter, rethRecipient), "pending message delivered before the relayer restart")

	// The message sent before the halt is only in the old blocks, so the
	// relayer first reads Celestia from the archive. It delivers messages in
	// nonce order, so it keeps one database across both phases.
	validator := chain.Validators[0]
	s.restartRelayerOn(ctx, agent, hyp, validator, archive, 0, relayerExportDB)
	s.Run("pending message delivered", func() {
		s.AssertERC20Balance(ctx, reth, tokenRouter, rethRecipient, erc20.Add(pending).BigInt())
	})
	// Then it follows the new chain from its first height.
	s.restartRelayerOn(ctx, agent, hyp, archive, validator, r.initialHeight, relayerExportDB)
	s.Run("replay rejected", func() {
		replay := &hypcoretypes.MsgProcessMessage{
			MailboxId: cfg.MailboxID,
			Relayer:   chain.GetFaucetWallet().GetFormattedAddress(),
			Message:   delivered.Message,
		}
		resp, err := cosmos.NewBroadcaster(chain).BroadcastMessages(ctx, chain.GetFaucetWallet(), replay)
		replayLog := resp.RawLog
		if err != nil {
			replayLog = err.Error()
		}
		s.Require().Contains(replayLog, "already received", "replayed message was not rejected")
	})
	s.Run("evm to celestia", func() {
		more := sdkmath.NewInt(200)
		s.SendTransferRemoteTxEvm(ctx, reth, tokenRouter, celestiaDomain, celestiaRecipientBz, more)
		s.AssertBankBalance(ctx, chain, celestiaRecipient, chain.Config.Denom, received.Add(more))
	})
	s.Run("celestia to evm", func() {
		more := sdkmath.NewInt(200)
		want := s.QueryERC20Balance(ctx, reth, tokenRouter, rethRecipient)
		s.SendTransferRemoteTx(ctx, chain, cfg.TokenID, rethDomain, rethRecipient, more)
		s.AssertERC20Balance(ctx, reth, tokenRouter, rethRecipient, want.Add(want, more.BigInt()))
	})
}

// processedMessage returns the message of the last MsgProcessMessage on Celestia.
func (s *HyperlaneTestSuite) processedMessage(ctx context.Context, chain *cosmos.Chain) *hypcoretypes.EventProcess {
	res, err := chain.GetNode().Client.TxSearch(ctx, "message.action='/hyperlane.core.v1.MsgProcessMessage'", false, nil, nil, "desc") //nolint:staticcheck // only tx_search finds the processed message
	s.Require().NoError(err, "failed to search process txs")
	s.Require().NotEmpty(res.Txs, "no process tx found")
	for _, e := range res.Txs[0].TxResult.Events {
		if e.Type != "hyperlane.core.v1.EventProcess" {
			continue
		}
		event, err := sdk.ParseTypedEvent(e)
		s.Require().NoError(err)
		return event.(*hypcoretypes.EventProcess)
	}
	s.Require().FailNow("no process event found")
	return nil
}

// hyperlaneState returns the Celestia state a warp transfer or a message can change.
func (s *HyperlaneTestSuite) hyperlaneState(ctx context.Context, chain *cosmos.Chain, cfg *hyperlane.CosmosConfig, recipient, messageID string) map[string]string {
	conn := chain.GetNode().GrpcConn
	denom := chain.Config.Denom
	warpModule := authtypes.NewModuleAddress(warptypes.ModuleName).String()

	supply, err := warptypes.NewQueryClient(conn).BridgedSupply(ctx, &warptypes.QueryBridgedSupplyRequest{Id: cfg.TokenID.String()})
	s.Require().NoError(err, "failed to query the collateral")
	mailbox, err := hypcoretypes.NewQueryClient(conn).Mailbox(ctx, &hypcoretypes.QueryMailboxRequest{Id: cfg.MailboxID.String()})
	s.Require().NoError(err, "failed to query the mailbox")
	delivered, err := hypcoretypes.NewQueryClient(conn).Delivered(ctx, &hypcoretypes.QueryDeliveredRequest{Id: cfg.MailboxID.String(), MessageId: messageID})
	s.Require().NoError(err, "failed to query the delivered set")

	balance := func(address string) string {
		amount, err := query.Balance(ctx, conn, address, denom)
		s.Require().NoError(err, "failed to query the balance of %s", address)
		return amount.String()
	}
	return map[string]string{
		"faucet":      balance(chain.GetFaucetWallet().GetFormattedAddress()),
		"recipient":   balance(recipient),
		"warp module": balance(warpModule),
		"collateral":  supply.BridgedSupply.String(),
		"sent":        fmt.Sprint(mailbox.Mailbox.MessageSent),
		"received":    fmt.Sprint(mailbox.Mailbox.MessageReceived),
		"delivered":   fmt.Sprint(delivered.Delivered),
	}
}

// restartRelayerOn restarts the relayer with its Celestia endpoints moved from
// one node to another and its database in the deployer directory db. indexFrom,
// if not zero, is the first Celestia height it indexes.
func (s *HyperlaneTestSuite) restartRelayerOn(ctx context.Context, agent *hyperlane.Agent, deployer *hyperlane.Deployer, from, to *cosmos.ChainNode, indexFrom int64, db string) {
	// The relayer container shares its volume with the deployer config.
	s.Require().NoError(agent.RemoveContainer(ctx, tastoratypes.WithPreserveVolumes()), "failed to remove the relayer")

	bz, err := deployer.ReadFile(ctx, "relayer-config.json")
	s.Require().NoError(err, "failed to read the relayer config")
	var relayerCfg map[string]any
	s.Require().NoError(json.Unmarshal(bz, &relayerCfg))
	chains, ok := relayerCfg["chains"].(map[string]any)
	s.Require().True(ok, "relayer config has no chains")
	celestiaCfg, ok := chains[HypCelestiaChainName].(map[string]any)
	s.Require().True(ok, "relayer config has no %s chain", HypCelestiaChainName)
	moved := 0
	for _, key := range []string{"rpcUrls", "grpcUrls", "restUrls"} {
		endpoints, _ := celestiaCfg[key].([]any)
		for _, e := range endpoints {
			endpoint, ok := e.(map[string]any)
			if !ok {
				continue
			}
			url, _ := endpoint["http"].(string)
			if strings.Contains(url, from.HostName()) {
				endpoint["http"] = strings.ReplaceAll(url, from.HostName(), to.HostName())
				moved++
			}
		}
	}
	s.Require().NotZero(moved, "no %s endpoint of the relayer points at %s", HypCelestiaChainName, from.HostName())
	if indexFrom > 0 {
		index, _ := celestiaCfg["index"].(map[string]any)
		if index == nil {
			index = map[string]any{}
		}
		index["from"] = indexFrom
		celestiaCfg["index"] = index
	}
	relayerCfg["db"] = deployer.HomeDir() + "/" + db
	bz, err = json.Marshal(relayerCfg)
	s.Require().NoError(err)
	s.Require().NoError(deployer.WriteFile(ctx, "relayer-config.json", bz), "failed to write the relayer config")

	s.Require().NoError(agent.Start(ctx), "failed to start the relayer on %s", to.Name())
}

// relayerExportDB is the relayer database directory of the export tests. It is
// on the deployer volume, which outlives the relayer container.
const relayerExportDB = "relayer-db-export"

// hyperlaneExportSetup is a Celestia chain with an archive node, an EVM chain
// and a relayer between them.
type hyperlaneExportSetup struct {
	chain          *cosmos.Chain
	archive        *cosmos.ChainNode
	reth           *EvolveEVMChain
	deployer       *hyperlane.Deployer
	cfg            *hyperlane.CosmosConfig
	tokenRouter    ethcommon.Address
	agent          *hyperlane.Agent
	rethDomain     uint32
	celestiaDomain uint32
}

// setupHyperlaneExport starts the chains and the relayer for an export test.
// The relayer database is relayerExportDB from the first start, so a restart
// can keep it.
func (s *HyperlaneTestSuite) setupHyperlaneExport(ctx context.Context) *hyperlaneExportSetup {
	t := s.T()
	chain, err := dockerchain.NewCelestiaChainBuilder(t, s.celestiaCfg).Build(ctx)
	s.Require().NoError(err)
	t.Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			t.Logf("Error removing chain: %v", err)
		}
	})
	s.Require().NoError(chain.Start(ctx))
	archive := s.addArchiveNode(ctx, chain)

	da := s.WithBridgeNodeNetwork(ctx, chain)
	reth := s.BuildEvolveEVMChain(ctx, s.BridgeNodeAddress(da), RethChainName0, RethChainID0)

	hyp, err := hyperlane.NewDeployer(ctx, hyperlane.Config{
		Logger:          s.logger,
		DockerClient:    s.client,
		DockerNetworkID: s.network,
		HyperlaneImage:  hyperlane.DefaultDeployerImage(),
	}, t.Name(), []hyperlane.ChainConfigProvider{reth, chain})
	s.Require().NoError(err)
	s.Require().NoError(hyp.Deploy(ctx))

	cfg, err := hyp.DeployCosmosNoopISM(ctx, cosmos.NewBroadcaster(chain), chain.GetFaucetWallet())
	s.Require().NoError(err)
	tokenRouter, err := hyp.GetEVMWarpTokenAddress()
	s.Require().NoError(err)
	s.EnrollRemoteRouters(ctx, chain, reth, hyp, tokenRouter, cfg.TokenID)

	s.setRelayerDB(ctx, hyp, relayerExportDB)
	agent := s.StartRelayerAgent(ctx, hyp)
	return &hyperlaneExportSetup{
		chain:          chain,
		archive:        archive,
		reth:           reth,
		deployer:       hyp,
		cfg:            cfg,
		tokenRouter:    tokenRouter,
		agent:          agent,
		rethDomain:     s.GetDomainForChain(ctx, reth.HyperlaneChainName(), hyp),
		celestiaDomain: s.GetDomainForChain(ctx, HypCelestiaChainName, hyp),
	}
}

// setRelayerDB sets the relayer database to the deployer directory db.
func (s *HyperlaneTestSuite) setRelayerDB(ctx context.Context, deployer *hyperlane.Deployer, db string) {
	bz, err := deployer.ReadFile(ctx, "relayer-config.json")
	s.Require().NoError(err, "failed to read the relayer config")
	var relayerCfg map[string]any
	s.Require().NoError(json.Unmarshal(bz, &relayerCfg))
	relayerCfg["db"] = deployer.HomeDir() + "/" + db
	bz, err = json.Marshal(relayerCfg)
	s.Require().NoError(err)
	s.Require().NoError(deployer.WriteFile(ctx, "relayer-config.json", bz), "failed to write the relayer config")
}
