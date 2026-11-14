package interop

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward"
	packetforwardkeeper "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/keeper"
	pfmtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	"github.com/cosmos/ibc-go/v8/modules/apps/transfer"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v8/modules/core/24-host"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/require"
)

// TestPFMTimeoutGivesUpOnOverLimitMemo covers a packet that celestia
// forwarded before the forward memo limits applied, with a forwarded memo
// outside them. When that packet times out, packet-forward-middleware must
// not decode the memo to retry it. It must give up as if no retries were
// left: the original sender gets the fixed error acknowledgement and its
// refund, once, and celestia keeps no vouchers.
func TestPFMTimeoutGivesUpOnOverLimitMemo(t *testing.T) {
	const fixedErrorAck = `{"error":"ABCI code: 11151: error handling packet: see events for details"}`

	coordinator, celestia, chainA, chainB := SetupTest(t)
	pathA := ibctesting.NewTransferPath(chainA, celestia)
	pathB := ibctesting.NewTransferPath(celestia, chainB)
	coordinator.Setup(pathA)
	coordinator.Setup(pathB)
	celestiaApp, ok := celestia.App.(*app.App)
	require.True(t, ok)
	chainAApp, ok := chainA.App.(*SimApp)
	require.True(t, ok)

	sender := chainA.SenderAccount.GetAddress()
	balanceBefore := chainAApp.BankKeeper.GetBalance(chainA.GetContext(), sender, sdk.DefaultBondDenom)

	next := strings.Repeat(`{"a":`, 1999) + `{}` + strings.Repeat(`}`, 1999) // 2000 levels
	memo := fmt.Sprintf(`{"forward":{"receiver":%q,"port":"transfer","channel":%q,"timeout":"1s","retries":5,"next":%s}}`,
		chainB.SenderAccount.GetAddress().String(), pathB.EndpointA.ChannelID, next)
	msg := transfertypes.NewMsgTransfer(pathA.EndpointA.ChannelConfig.PortID, pathA.EndpointA.ChannelID,
		sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1000)), sender.String(), "unused",
		clienttypes.NewHeight(1, 100000), 0, memo)
	res, err := chainA.SendMsgs(msg)
	require.NoError(t, err)
	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
	require.NoError(t, err)

	// Celestia receives the packet and forwards it with
	// packet-forward-middleware alone, as a node did before the limits.
	require.NoError(t, pathA.EndpointB.UpdateClient())
	proof, proofHeight := chainA.QueryProof(host.PacketCommitmentKey(packet.SourcePort, packet.SourceChannel, packet.Sequence))
	ctx := celestia.GetContext()
	chanCap, ok := celestiaApp.ScopedIBCKeeper.GetCapability(ctx, host.ChannelCapabilityPath(packet.DestinationPort, packet.DestinationChannel))
	require.True(t, ok)
	require.NoError(t, celestiaApp.IBCKeeper.ChannelKeeper.RecvPacket(ctx, chanCap, packet, proof, proofHeight))
	pfmOnly := packetforward.NewIBCMiddleware(transfer.NewIBCModule(celestiaApp.TransferKeeper), celestiaApp.PacketForwardKeeper,
		0, packetforwardkeeper.DefaultForwardTransferPacketTimeoutTimestamp)
	require.Nil(t, pfmOnly.OnRecvPacket(ctx, packet, celestia.SenderAccount.GetAddress()), "the packet must be forwarded")
	fwd, err := ibctesting.ParsePacketFromEvents(ctx.EventManager().ABCIEvents())
	require.NoError(t, err)
	coordinator.CommitBlock(celestia)

	// The forwarded packet times out.
	coordinator.IncrementTimeBy(time.Minute)
	coordinator.CommitBlock(chainB)
	require.NoError(t, pathB.EndpointA.UpdateClient())
	proof, proofHeight = chainB.QueryProof(host.PacketReceiptKey(fwd.DestinationPort, fwd.DestinationChannel, fwd.Sequence))
	nextSeqRecv, found := chainB.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceRecv(chainB.GetContext(), fwd.DestinationPort, fwd.DestinationChannel)
	require.True(t, found)
	timeoutRes, err := celestia.SendMsgs(channeltypes.NewMsgTimeout(fwd, nextSeqRecv, proof, proofHeight, celestia.SenderAccount.GetAddress().String()))
	require.NoError(t, err)
	_, err = ibctesting.ParsePacketFromEvents(timeoutRes.GetEvents())
	require.Error(t, err, "celestia must not retry the forwarded packet")

	ctx = celestia.GetContext()
	require.False(t, ctx.KVStore(celestiaApp.GetKey(pfmtypes.StoreKey)).Has(
		pfmtypes.RefundPacketKey(fwd.SourceChannel, fwd.SourcePort, fwd.Sequence)), "the in-flight packet must be removed")
	ack, err := ibctesting.ParseAckFromEvents(timeoutRes.GetEvents())
	require.NoError(t, err, "celestia must write the ack of the original packet")
	require.Equal(t, fixedErrorAck, string(ack))
	commitment, found := celestiaApp.IBCKeeper.ChannelKeeper.GetPacketAcknowledgement(ctx, packet.DestinationPort, packet.DestinationChannel, packet.Sequence)
	require.True(t, found)
	require.Equal(t, channeltypes.CommitAcknowledgement(ack), commitment)

	// Celestia burned the vouchers it minted for the original packet.
	voucher := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom(
		packet.DestinationPort, packet.DestinationChannel, sdk.DefaultBondDenom)).IBCDenom()
	require.True(t, celestiaApp.BankKeeper.GetSupply(ctx, voucher).IsZero(), "celestia must hold no vouchers")
	require.True(t, celestiaApp.TransferKeeper.GetTotalEscrowForDenom(ctx, voucher).IsZero())

	// Chain A refunds the sender once when it receives the error ack.
	require.NoError(t, pathA.EndpointA.UpdateClient())
	require.NoError(t, pathA.EndpointA.AcknowledgePacket(packet, ack))
	require.Equal(t, balanceBefore, chainAApp.BankKeeper.GetBalance(chainA.GetContext(), sender, sdk.DefaultBondDenom))
	// Relaying the ack again is a no-op.
	require.NoError(t, pathA.EndpointA.AcknowledgePacket(packet, ack))
	require.Equal(t, balanceBefore, chainAApp.BankKeeper.GetBalance(chainA.GetContext(), sender, sdk.DefaultBondDenom))
}

// TestPFMTimeoutOfOwnTransferRefunds checks that a transfer sent from
// celestia, not forwarded, is refunded when it times out, also when its memo
// is outside the retry limits.
func TestPFMTimeoutOfOwnTransferRefunds(t *testing.T) {
	coordinator, celestia, _, chainB := SetupTest(t)
	path := ibctesting.NewTransferPath(celestia, chainB)
	coordinator.Setup(path)
	celestiaApp, ok := celestia.App.(*app.App)
	require.True(t, ok)
	sender := celestia.SenderAccount.GetAddress()

	for _, memo := range []string{"", "hello", `{"a":1,"a":2}`} {
		balanceBefore := celestiaApp.BankKeeper.GetBalance(celestia.GetContext(), sender, sdk.DefaultBondDenom)
		timeout := uint64(celestia.CurrentHeader.Time.Add(time.Second).UnixNano())
		msg := transfertypes.NewMsgTransfer(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
			sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1000)), sender.String(), chainB.SenderAccount.GetAddress().String(),
			clienttypes.ZeroHeight(), timeout, memo)
		res, err := celestia.SendMsgs(msg)
		require.NoError(t, err)
		packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
		require.NoError(t, err)

		coordinator.IncrementTimeBy(time.Minute)
		coordinator.CommitBlock(chainB)
		require.NoError(t, path.EndpointA.UpdateClient())
		require.NoError(t, path.EndpointA.TimeoutPacket(packet), memo)
		require.Equal(t, balanceBefore, celestiaApp.BankKeeper.GetBalance(celestia.GetContext(), sender, sdk.DefaultBondDenom), memo)
	}
}
