package interop

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/require"
)

// TestPFMAsyncAckIsFixed covers the acknowledgements that
// packet-forward-middleware writes for the original packet after it has
// forwarded it: when the forwarded packet times out, and when the next chain
// returns an error acknowledgement. The committed acknowledgement must not
// hold Go error text.
func TestPFMAsyncAckIsFixed(t *testing.T) {
	const fixedErrorAck = `{"error":"ABCI code: 11151: error handling packet: see events for details"}`

	testCases := []struct {
		name string
		// next is the JSON value of forward.next, empty to omit it.
		next string
		// timeout is the JSON value of forward.timeout.
		timeout string
		// timeOut makes the forwarded packet time out instead of relaying it.
		timeOut bool
		// invalidReceiver makes the transfer on chain B fail.
		invalidReceiver bool
		// wantAck is the ack celestia commits for the original packet. Empty
		// means the ack of chain B, relayed unchanged.
		wantAck string
	}{
		{
			name:    "forwarded packet times out with no retries left",
			timeout: `"1s"`,
			timeOut: true,
			wantAck: fixedErrorAck,
		},
		{
			// The next chain runs packet-forward-middleware without the
			// celestia-app wrapper, so its error ack holds Go error text.
			name:    "next chain returns a packet-forward-middleware error ack",
			next:    `{"forward":{"receiver":"r","port":"transfer","channel":"channel-999"}}`,
			timeout: `"10m"`,
			wantAck: fixedErrorAck,
		},
		{
			// ibc-go error acks hold only the ABCI code, so celestia
			// relays them unchanged.
			name:            "next chain returns a transfer error ack",
			timeout:         `"10m"`,
			invalidReceiver: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			coordinator, celestia, chainA, chainB := SetupTest(t)
			pathA := ibctesting.NewTransferPath(chainA, celestia)
			pathB := ibctesting.NewTransferPath(celestia, chainB)
			coordinator.Setup(pathA)
			coordinator.Setup(pathB)

			receiver := chainB.SenderAccount.GetAddress().String()
			if tc.invalidReceiver {
				receiver = "invalid receiver"
			}
			next := ""
			if tc.next != "" {
				next = `,"next":` + tc.next
			}
			memo := fmt.Sprintf(`{"forward":{"receiver":%q,"port":"transfer","channel":%q,"timeout":%s,"retries":0%s}}`,
				receiver, pathB.EndpointA.ChannelID, tc.timeout, next)
			msg := transfertypes.NewMsgTransfer(pathA.EndpointA.ChannelConfig.PortID, pathA.EndpointA.ChannelID,
				sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1000)), chainA.SenderAccount.GetAddress().String(), "unused",
				clienttypes.NewHeight(1, 100000), 0, memo)
			res, err := chainA.SendMsgs(msg)
			require.NoError(t, err)
			packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
			require.NoError(t, err)

			// Celestia receives the packet and forwards it to chain B.
			require.NoError(t, pathA.EndpointB.UpdateClient())
			recvRes, err := pathA.EndpointB.RecvPacketWithResult(packet)
			require.NoError(t, err)
			_, err = ibctesting.ParseAckFromEvents(recvRes.GetEvents())
			require.Error(t, err, "celestia must forward the packet and write no ack yet")
			fwd, err := ibctesting.ParsePacketFromEvents(recvRes.GetEvents())
			require.NoError(t, err)

			wantAck := tc.wantAck
			if tc.timeOut {
				coordinator.IncrementTimeBy(time.Minute)
				coordinator.CommitBlock(chainB)
				require.NoError(t, pathB.EndpointA.UpdateClient())
				require.NoError(t, pathB.EndpointA.TimeoutPacket(fwd))
			} else {
				require.NoError(t, pathB.EndpointB.UpdateClient())
				recvRes, err := pathB.EndpointB.RecvPacketWithResult(fwd)
				require.NoError(t, err)
				ack, err := ibctesting.ParseAckFromEvents(recvRes.GetEvents())
				require.NoError(t, err)
				require.Contains(t, string(ack), `"error"`, "chain B must return an error ack")
				if wantAck == "" {
					wantAck = string(ack)
				}
				require.NoError(t, pathB.EndpointA.UpdateClient())
				require.NoError(t, pathB.EndpointA.AcknowledgePacket(fwd, ack))
			}

			// Celestia has now written the ack of the original packet.
			commitment, found := celestia.App.GetIBCKeeper().ChannelKeeper.GetPacketAcknowledgement(
				celestia.GetContext(), packet.DestinationPort, packet.DestinationChannel, packet.Sequence)
			require.True(t, found, "celestia must write the ack of the original packet")
			require.Equal(t, channeltypes.CommitAcknowledgement([]byte(wantAck)), commitment)
		})
	}
}
