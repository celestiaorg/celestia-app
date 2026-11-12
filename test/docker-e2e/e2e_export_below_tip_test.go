package docker_e2e

import (
	"context"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	sdktx "github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v8/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	commitmenttypes "github.com/cosmos/ibc-go/v8/modules/core/23-commitment/types"
	host "github.com/cosmos/ibc-go/v8/modules/core/24-host"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"celestiaorg/celestia-app/test/docker-e2e/internal"
)

// TestIBCExportBelowTip restarts Celestia from an export below the last height
// Hermes relayed. Packets relayed in the dropped blocks are checked after the
// restart: X (Celestia to simapp) stays received on simapp, Y (simapp to
// Celestia, acknowledged) is lost, Z (simapp to Celestia, not acknowledged) is
// received again once, and a new packet W on X's sequence can be neither
// received nor timed out, but X's old acknowledgement deletes it.
func (s *IBCTestSuite) TestIBCExportBelowTip() {
	if testing.Short() {
		s.T().Skip("skipping IBC export below tip test in short mode")
	}
	ctx := context.Background()

	s.setupIBCInfrastructure(appconsts.Version, true)
	archive := s.addArchiveNode(ctx, s.chainA)
	s.Require().NoError(s.hermes.Start(ctx), "failed to start hermes")
	s.testTokenTransfers(ctx, s.transferChannel)
	s.testReverseTokenTransfers(ctx, s.transferChannel)

	ch := s.transferChannel
	celestiaID := s.chainA.GetChainID()
	s.waitUntil(ctx, 90*time.Second, "packets are still in flight", func(ctx context.Context) bool {
		return len(s.packetCommitments(ctx, s.chainA, ch.PortID, ch.ChannelID)) == 0 &&
			len(s.packetCommitments(ctx, s.chainB, ch.CounterpartyPort, ch.CounterpartyID)) == 0
	})
	// Hermes.Stop deletes the config volume, so stop the container only.
	s.Require().NoError(s.hermes.StopContainer(ctx), "failed to stop hermes")

	celestiaFaucet := s.chainA.GetFaucetWallet().GetFormattedAddress()
	simappFaucet := s.chainB.GetFaucetWallet().GetFormattedAddress()
	celestiaEscrow := ibctransfertypes.GetEscrowAddress(ch.PortID, ch.ChannelID).String()
	simappEscrow := ibctransfertypes.GetEscrowAddress(ch.CounterpartyPort, ch.CounterpartyID).String()
	utiaVoucher := s.calculateIBCDenom(ch.CounterpartyPort, ch.CounterpartyID, "utia")
	stakeVoucher := s.calculateIBCDenom(ch.PortID, ch.ChannelID, "stake")
	simappUtia := func(ctx context.Context) sdkmath.Int { return s.getBalance(ctx, s.chainB, simappFaucet, utiaVoucher) }
	celestiaStake := func(ctx context.Context) sdkmath.Int {
		return s.getBalance(ctx, s.chainA, celestiaFaucet, stakeVoucher)
	}

	// Snapshot at the export height E.
	before := s.ibcBalances(ctx, "")
	n := s.nextSequenceSend(ctx, s.chainA, ch.PortID, ch.ChannelID)
	simappUtiaBefore := simappUtia(ctx)
	celestiaStakeBefore := celestiaStake(ctx)
	celestiaEscrowBefore := s.getBalance(ctx, s.chainA, celestiaEscrow, "utia")
	simappEscrowBefore := s.getBalance(ctx, s.chainB, simappEscrow, "stake")
	exportAt, err := s.chainA.Validators[0].Height(ctx)
	s.Require().NoError(err)

	// Blocks above E, relayed with one-shot Hermes commands.
	x, y, z := sdkmath.NewInt(7000), sdkmath.NewInt(5000), sdkmath.NewInt(3000)
	s.sendTransfer(ctx, x, time.Now().Add(time.Hour))
	s.hermesTx(ctx, "packet-recv", simappChainID, celestiaID, ch.PortID, ch.ChannelID)
	s.waitUntil(ctx, time.Minute, "X was not received on simapp", func(ctx context.Context) bool {
		return simappUtia(ctx).Equal(simappUtiaBefore.Add(x))
	})
	s.hermesTx(ctx, "packet-ack", celestiaID, simappChainID, ch.CounterpartyPort, ch.CounterpartyID)
	s.waitUntil(ctx, time.Minute, "X was not acknowledged on Celestia", func(ctx context.Context) bool {
		return !s.packetCommitted(ctx, s.chainA, ch.PortID, ch.ChannelID, n)
	})

	m := s.nextSequenceSend(ctx, s.chainB, ch.CounterpartyPort, ch.CounterpartyID)
	s.sendFromSimapp(ctx, y)
	s.hermesTx(ctx, "packet-recv", celestiaID, simappChainID, ch.CounterpartyPort, ch.CounterpartyID)
	s.waitUntil(ctx, time.Minute, "Y was not received on Celestia", func(ctx context.Context) bool {
		return celestiaStake(ctx).Equal(celestiaStakeBefore.Add(y))
	})
	s.hermesTx(ctx, "packet-ack", simappChainID, celestiaID, ch.PortID, ch.ChannelID)
	s.waitUntil(ctx, time.Minute, "Y was not acknowledged on simapp", func(ctx context.Context) bool {
		return !s.packetCommitted(ctx, s.chainB, ch.CounterpartyPort, ch.CounterpartyID, m)
	})

	s.sendFromSimapp(ctx, z)
	s.hermesTx(ctx, "packet-recv", celestiaID, simappChainID, ch.CounterpartyPort, ch.CounterpartyID)
	s.waitUntil(ctx, time.Minute, "Z was not received on Celestia", func(ctx context.Context) bool {
		return celestiaStake(ctx).Equal(celestiaStakeBefore.Add(y).Add(z))
	})

	r := &exportRestart{exportAt: exportAt, archive: archive}
	s.exportAndRestart(ctx, s.chainA, r)
	s.T().Logf("exported at height %d, new chain starts at %d", r.exportHeight, r.initialHeight)
	s.celestiaTxClient, err = s.setupTxClient(ctx, s.chainA)
	s.Require().NoError(err, "failed to set up the tx client on the new chain")
	celestiaClientID := s.celestiaClientID(ctx)
	s.updateClientAcrossRestart(ctx, celestiaClientID, archive)
	s.requireCelestiaClientActive(ctx)

	s.Require().Equal(before, s.ibcBalances(ctx, ""), "balances differ from the export height")
	s.Require().Equal(n, s.nextSequenceSend(ctx, s.chainA, ch.PortID, ch.ChannelID), "send sequence differs from the export height")
	s.Require().True(s.packetReceived(ctx, s.chainB, ch.CounterpartyPort, ch.CounterpartyID, n), "simapp has no receipt of X")
	// X is unbacked: simapp keeps the voucher, Celestia no longer escrows it.
	s.Require().Equal(simappUtiaBefore.Add(x), simappUtia(ctx), "simapp lost the voucher of X")
	// Y is lost: simapp escrows it without a commitment, Celestia never minted it.
	s.Require().False(s.packetCommitted(ctx, s.chainB, ch.CounterpartyPort, ch.CounterpartyID, m), "simapp has a commitment of Y")
	s.Require().Equal(celestiaStakeBefore, celestiaStake(ctx), "Celestia has the voucher of Y or Z")
	s.Require().Equal(simappEscrowBefore.Add(y).Add(z), s.getBalance(ctx, s.chainB, simappEscrow, "stake"), "simapp escrow does not hold Y and Z")

	// Z is still committed on simapp, so it is received again, once.
	s.hermesTx(ctx, "packet-recv", celestiaID, simappChainID, ch.CounterpartyPort, ch.CounterpartyID)
	s.waitUntil(ctx, time.Minute, "Z was not received on the new chain", func(ctx context.Context) bool {
		return celestiaStake(ctx).Equal(celestiaStakeBefore.Add(z))
	})
	s.hermesTx(ctx, "packet-ack", simappChainID, celestiaID, ch.PortID, ch.ChannelID)
	s.waitUntil(ctx, time.Minute, "Z was not acknowledged on simapp", func(ctx context.Context) bool {
		return !s.packetCommitted(ctx, s.chainB, ch.CounterpartyPort, ch.CounterpartyID, m+1)
	})
	s.Require().Equal(celestiaStakeBefore.Add(z), celestiaStake(ctx), "Z was received more than once or Y was received")

	// W reuses X's sequence n.
	w := sdkmath.NewInt(3000)
	wTimeout := time.Now().Add(3 * time.Minute)
	wResp := s.sendTransfer(ctx, w, wTimeout)
	s.waitForAppHeight(ctx, s.chainA.GetNode(), wResp.Height)
	s.Require().Equal(n+1, s.nextSequenceSend(ctx, s.chainA, ch.PortID, ch.ChannelID), "W did not use sequence n")
	wData := ibctransfertypes.NewFungibleTokenPacketData("utia", w.String(), celestiaFaucet, simappFaucet, "").GetBytes()
	packet := channeltypes.NewPacket(wData, n, ch.PortID, ch.ChannelID, ch.CounterpartyPort, ch.CounterpartyID, clienttypes.ZeroHeight(), uint64(wTimeout.UnixNano()))

	s.Run("W is not received", func() {
		// The receive must reach simapp before W times out.
		prepCtx, cancel := context.WithDeadline(ctx, wTimeout.Add(-30*time.Second))
		defer cancel()
		proof, proofHeight := s.ibcProof(prepCtx, s.chainA.Validators[0], celestiaID, host.PacketCommitmentKey(ch.PortID, ch.ChannelID, n), wResp.Height)
		s.updateHostedClient(prepCtx, s.chainB, celestiaClientID, s.chainA.Validators[0], proofHeight)
		s.Require().NoError(prepCtx.Err(), "receive of W was not ready before its timeout")
		b := cosmos.NewBroadcaster(s.chainB)
		b.ConfigureFactoryOptions(func(f sdktx.Factory) sdktx.Factory { return f.WithGas(400000) })
		resp, err := b.BroadcastMessages(ctx, s.chainB.GetFaucetWallet(), channeltypes.NewMsgRecvPacket(packet, proof, proofHeight, simappFaucet))
		if err != nil {
			s.Require().Contains(err.Error(), "redundant", "MsgRecvPacket of W failed for another reason")
		} else {
			bz, err := hex.DecodeString(resp.Data)
			s.Require().NoError(err)
			var data sdk.TxMsgData
			s.Require().NoError(data.Unmarshal(bz))
			s.Require().Len(data.MsgResponses, 1)
			var recv channeltypes.MsgRecvPacketResponse
			s.Require().NoError(recv.Unmarshal(data.MsgResponses[0].Value))
			s.Require().Equal(channeltypes.NOOP, recv.Result, "MsgRecvPacket of W was not a no-op")
		}
		s.Require().Equal(simappUtiaBefore.Add(x), simappUtia(ctx), "W was received on a reused sequence")
	})

	// Pin one simapp height past W's timeout; the timeout and the ack replay
	// both prove against it.
	var pinned int64
	err = s.WaitForSync(ctx, s.chainB.GetNode().Client, time.Until(wTimeout)+2*time.Minute, func(info coretypes.SyncInfo) bool {
		pinned = info.LatestBlockHeight
		return info.LatestBlockTime.After(wTimeout)
	})
	s.Require().NoError(err, "simapp did not pass the timeout of W")
	simapp := s.chainB.GetNode()
	receiptProof, proofHeight := s.ibcProof(ctx, simapp, simappChainID, host.PacketReceiptKey(ch.CounterpartyPort, ch.CounterpartyID, n), pinned)
	ackProof, _ := s.ibcProof(ctx, simapp, simappChainID, host.PacketAcknowledgementKey(ch.CounterpartyPort, ch.CounterpartyID, n), pinned)
	s.updateHostedClient(ctx, s.chainA, s.simappClientID(ctx), simapp, proofHeight)

	s.Run("W is not timed out", func() {
		timeout := channeltypes.NewMsgTimeout(packet, n, receiptProof, proofHeight, celestiaFaucet)
		_, err := s.celestiaTxClient.SubmitTx(ctx, []sdk.Msg{timeout}, user.SetGasLimit(400000), user.SetFee(10000))
		s.Require().ErrorContains(err, "failed packet receipt absence verification")
		s.Require().True(s.packetCommitted(ctx, s.chainA, ch.PortID, ch.ChannelID, n), "commitment of W was deleted")
		s.Require().Equal(celestiaEscrowBefore.Add(w), s.getBalance(ctx, s.chainA, celestiaEscrow, "utia"), "W was refunded")
	})

	s.Run("old ack deletes W", func() {
		ack := channeltypes.NewResultAcknowledgement([]byte{1}).Acknowledgement()
		msg := channeltypes.NewMsgAcknowledgement(packet, ack, ackProof, proofHeight, celestiaFaucet)
		resp, err := s.celestiaTxClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(400000), user.SetFee(10000))
		s.Require().NoError(err, "replayed ack of X was rejected")
		s.Require().Equal(uint32(0), resp.Code)
		s.Require().False(s.packetCommitted(ctx, s.chainA, ch.PortID, ch.ChannelID, n), "commitment of W was not deleted")
		s.Require().Equal(celestiaEscrowBefore.Add(w), s.getBalance(ctx, s.chainA, celestiaEscrow, "utia"), "W was refunded")
	})

	s.Require().NoError(s.hermes.StartContainer(ctx), "failed to restart hermes")
	s.Run("celestia to simapp", func() {
		s.testTokenTransfers(ctx, ch)
		s.Require().Equal(n+2, s.nextSequenceSend(ctx, s.chainA, ch.PortID, ch.ChannelID))
	})
	s.Run("simapp to celestia", func() { s.testReverseTokenTransfers(ctx, ch) })
	s.Run("client active", func() { s.requireCelestiaClientActive(ctx) })
}

// waitUntil waits up to timeout for cond and fails with msg otherwise.
func (s *IBCTestSuite) waitUntil(ctx context.Context, timeout time.Duration, msg string, cond func(context.Context) bool) {
	err := internal.WaitFor(ctx, timeout, time.Second, func(ctx context.Context) (bool, error) {
		return cond(ctx), nil
	})
	s.Require().NoError(err, msg)
}

// hermesTx relays packets or acks from src to dst with a one-shot Hermes command.
func (s *IBCTestSuite) hermesTx(ctx context.Context, kind, dst, src, srcPort, srcChannel string) {
	s.hermesExec(ctx, "tx", kind, "--dst-chain", dst, "--src-chain", src, "--src-port", srcPort, "--src-channel", srcChannel)
}

// sendFromSimapp sends stake from simapp to Celestia without waiting for delivery.
func (s *IBCTestSuite) sendFromSimapp(ctx context.Context, amount sdkmath.Int) {
	msg := ibctransfertypes.NewMsgTransfer(
		s.transferChannel.CounterpartyPort,
		s.transferChannel.CounterpartyID,
		sdk.NewCoin("stake", amount),
		s.chainB.GetFaucetWallet().GetFormattedAddress(),
		s.chainA.GetFaucetWallet().GetFormattedAddress(),
		clienttypes.ZeroHeight(),
		uint64(time.Now().Add(time.Hour).UnixNano()),
		"",
	)
	resp, err := cosmos.NewBroadcaster(s.chainB).BroadcastMessages(ctx, s.chainB.GetFaucetWallet(), msg)
	s.Require().NoError(err, "failed to send from simapp")
	s.Require().Equal(uint32(0), resp.Code, "simapp transfer failed: %s", resp.RawLog)
}

// nextSequenceSend returns the next send sequence of a channel.
func (s *IBCTestSuite) nextSequenceSend(ctx context.Context, chain *cosmos.Chain, port, channel string) uint64 {
	res, err := channeltypes.NewQueryClient(chain.GetNode().GrpcConn).NextSequenceSend(ctx, &channeltypes.QueryNextSequenceSendRequest{PortId: port, ChannelId: channel})
	s.Require().NoError(err, "failed to query the next send sequence")
	return res.NextSequenceSend
}

// packetCommitments returns the packet commitments of a channel.
func (s *IBCTestSuite) packetCommitments(ctx context.Context, chain *cosmos.Chain, port, channel string) []*channeltypes.PacketState {
	res, err := channeltypes.NewQueryClient(chain.GetNode().GrpcConn).PacketCommitments(ctx, &channeltypes.QueryPacketCommitmentsRequest{PortId: port, ChannelId: channel})
	s.Require().NoError(err, "failed to query the packet commitments")
	return res.Commitments
}

// packetCommitted reports whether a channel has a commitment for the sequence.
func (s *IBCTestSuite) packetCommitted(ctx context.Context, chain *cosmos.Chain, port, channel string, seq uint64) bool {
	_, err := channeltypes.NewQueryClient(chain.GetNode().GrpcConn).PacketCommitment(ctx, &channeltypes.QueryPacketCommitmentRequest{PortId: port, ChannelId: channel, Sequence: seq})
	if status.Code(err) == codes.NotFound {
		return false
	}
	s.Require().NoError(err, "failed to query the packet commitment")
	return true
}

// packetReceived reports whether a channel has a receipt for the sequence.
func (s *IBCTestSuite) packetReceived(ctx context.Context, chain *cosmos.Chain, port, channel string, seq uint64) bool {
	res, err := channeltypes.NewQueryClient(chain.GetNode().GrpcConn).PacketReceipt(ctx, &channeltypes.QueryPacketReceiptRequest{PortId: port, ChannelId: channel, Sequence: seq})
	s.Require().NoError(err, "failed to query the packet receipt")
	return res.Received
}

// simappClientID returns the id of Celestia's client of simapp.
func (s *IBCTestSuite) simappClientID(ctx context.Context) string {
	connection, err := connectiontypes.NewQueryClient(s.chainA.GetNode().GrpcConn).Connection(ctx, &connectiontypes.QueryConnectionRequest{ConnectionId: s.connection.ConnectionID})
	s.Require().NoError(err, "failed to query the Celestia connection")
	return connection.Connection.ClientId
}

// ibcProof returns a proof of an IBC store key in the state at height and the
// client height that verifies it, height + 1.
func (s *IBCTestSuite) ibcProof(ctx context.Context, node *cosmos.ChainNode, chainID string, key []byte, height int64) ([]byte, clienttypes.Height) {
	s.waitForAppHeight(ctx, node, height)
	res, err := node.Client.ABCIQueryWithOptions(ctx, "store/"+ibcexported.StoreKey+"/key", key, rpcclient.ABCIQueryOptions{Height: height, Prove: true})
	s.Require().NoError(err, "failed to query %s at height %d", key, height)
	s.Require().NotEmpty(res.Response.Value, "%s is not set at height %d", key, height)
	merkleProof, err := commitmenttypes.ConvertProofs(res.Response.ProofOps)
	s.Require().NoError(err)
	proof, err := merkleProof.Marshal()
	s.Require().NoError(err)
	return proof, clienttypes.NewHeight(clienttypes.ParseChainID(chainID), uint64(res.Response.Height)+1)
}

// waitForAppHeight waits until the node's application has committed height.
// The RPC status height can run ahead of it.
func (s *IBCTestSuite) waitForAppHeight(ctx context.Context, node *cosmos.ChainNode, height int64) {
	s.waitUntil(ctx, 30*time.Second, "application did not commit the height", func(ctx context.Context) bool {
		info, err := node.Client.ABCIInfo(ctx)
		return err == nil && info.Response.LastBlockHeight >= height
	})
}

// updateHostedClient updates a client on the host chain to height with a
// one-shot Hermes command, once the reference node has that block.
func (s *IBCTestSuite) updateHostedClient(ctx context.Context, hostChain *cosmos.Chain, clientID string, reference *cosmos.ChainNode, height clienttypes.Height) {
	err := s.WaitForSync(ctx, reference.Client, time.Minute, func(info coretypes.SyncInfo) bool {
		return info.LatestBlockHeight >= int64(height.RevisionHeight)
	})
	s.Require().NoError(err, "reference chain did not reach height %d", height.RevisionHeight)
	s.hermesExec(ctx, "update", "client", "--host-chain", hostChain.GetChainID(), "--client", clientID, "--height", strconv.FormatUint(height.RevisionHeight, 10))
	client := clienttypes.NewQueryClient(hostChain.GetNode().GrpcConn)
	s.waitUntil(ctx, 30*time.Second, "client has no consensus state at the update height", func(ctx context.Context) bool {
		_, err := client.ConsensusState(ctx, &clienttypes.QueryConsensusStateRequest{ClientId: clientID, RevisionNumber: height.RevisionNumber, RevisionHeight: height.RevisionHeight})
		return err == nil
	})
}
