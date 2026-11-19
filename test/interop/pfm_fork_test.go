package interop

import (
	"encoding/json"
	"fmt"
	"strings"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
)

// pfmFixedErrorAck is the error acknowledgement that celestia-app commits for
// every packet-forward-middleware error.
const pfmFixedErrorAck = `{"error":"packet-forward-middleware error: forward failed"}`

// TestForwardedMemoIsNextAsSent verifies that Celestia forwards the "next"
// memo with the bytes the sender chose. Upstream packet-forward-middleware
// v8.2.0 decodes and re-encodes "next": it removes whitespace and escapes
// '<', so the forwarded bytes depend on the Go JSON encoder.
func (s *PacketForwardMiddlewareTestSuite) TestForwardedMemoIsNextAsSent() {
	next := `{"note": "a<b",  "amount": 1e20}`
	forward := &PacketMetadata{
		Forward: &ForwardMetadata{
			Receiver: s.chainB.SenderAccount.GetAddress().String(),
			Port:     s.pathCelestiaToB.EndpointA.ChannelConfig.PortID,
			Channel:  s.pathCelestiaToB.EndpointA.ChannelID,
			Next:     &next,
		},
	}
	memo, err := json.Marshal(forward)
	s.Require().NoError(err)

	msg := transfertypes.NewMsgTransfer(
		s.pathAToCelestia.EndpointA.ChannelConfig.PortID,
		s.pathAToCelestia.EndpointA.ChannelID,
		sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1000)),
		s.chainA.SenderAccount.GetAddress().String(),
		s.celestia.SenderAccount.GetAddress().String(),
		clienttypes.NewHeight(1, 300), 0, string(memo),
	)
	res, err := s.chainA.SendMsgs(msg)
	s.Require().NoError(err)
	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
	s.Require().NoError(err)

	forwarded, ack, err := relayPacket(s.pathAToCelestia.EndpointB, packet)
	s.Require().NoError(err)
	s.Require().Empty(ack, "Celestia must forward the packet, not acknowledge it")

	var data transfertypes.FungibleTokenPacketData
	s.Require().NoError(transfertypes.ModuleCdc.UnmarshalJSON(forwarded.GetData(), &data))
	s.Require().Equal(next, data.Memo)
}

// TestForwardMemoLengthLimit verifies that Celestia forwards a packet with a
// forward memo of exactly transfertypes.MaximumMemoLength bytes and rejects a
// longer one with the fixed error acknowledgement. ibc-go senders cannot
// create a longer memo (MsgTransfer.ValidateBasic refuses it), so the packet
// goes straight to the transfer stack, as from another IBC implementation.
// Upstream packet-forward-middleware v8.2.0 forwards both.
func (s *PacketForwardMiddlewareTestSuite) TestForwardMemoLengthLimit() {
	celestiaApp := s.GetCelestiaApp(s.celestia)
	transferStack, ok := celestiaApp.IBCKeeper.Router.GetRoute(transfertypes.ModuleName)
	s.Require().True(ok)

	testCases := []struct {
		name    string
		length  int
		wantAck string // empty: the packet is forwarded
	}{
		{name: "at limit is forwarded", length: transfertypes.MaximumMemoLength},
		{name: "over limit is rejected", length: transfertypes.MaximumMemoLength + 1, wantAck: pfmFixedErrorAck},
	}
	for i, tc := range testCases {
		s.Run(tc.name, func() {
			memo := s.paddedForwardMemo(tc.length)
			s.Require().Len(memo, tc.length)

			data := transfertypes.NewFungibleTokenPacketData(
				sdk.DefaultBondDenom, "1000",
				s.chainA.SenderAccount.GetAddress().String(),
				s.celestia.SenderAccount.GetAddress().String(),
				memo,
			)
			packet := channeltypes.NewPacket(
				data.GetBytes(), uint64(1000+i),
				s.pathAToCelestia.EndpointA.ChannelConfig.PortID, s.pathAToCelestia.EndpointA.ChannelID,
				s.pathAToCelestia.EndpointB.ChannelConfig.PortID, s.pathAToCelestia.EndpointB.ChannelID,
				clienttypes.NewHeight(1, 300), 0,
			)

			ack := transferStack.OnRecvPacket(s.celestia.GetContext(), packet, s.celestia.SenderAccount.GetAddress())
			if tc.wantAck == "" {
				s.Require().Nil(ack, "a forwarded packet is acknowledged later")
				return
			}
			s.Require().NotNil(ack)
			s.Require().False(ack.Success())
			s.Require().Equal(tc.wantAck, string(ack.Acknowledgement()))
		})
	}
}

// paddedForwardMemo returns a valid forward memo to chain B of exactly length
// bytes. The padding is outside the forward metadata.
func (s *PacketForwardMiddlewareTestSuite) paddedForwardMemo(length int) string {
	forward := fmt.Sprintf(`"forward":{"receiver":%q,"port":%q,"channel":%q}`,
		s.chainB.SenderAccount.GetAddress().String(),
		s.pathCelestiaToB.EndpointA.ChannelConfig.PortID,
		s.pathCelestiaToB.EndpointA.ChannelID,
	)
	prefix := `{` + forward + `,"pad":"`
	suffix := `"}`
	return prefix + strings.Repeat("x", length-len(prefix)-len(suffix)) + suffix
}
