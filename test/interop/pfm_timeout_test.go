package interop

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	sdk "github.com/cosmos/cosmos-sdk/types"
	pfmtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v8/modules/core/24-host"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/require"
)

// TestPFMForwardTimeout checks the committed result of a forward memo for
// each timeout value. A numeric timeout must be an int64 number of
// nanoseconds. A number outside the int64 range gets the fixed
// packet-forward-middleware error acknowledgement and is not forwarded.
func TestPFMForwardTimeout(t *testing.T) {
	const (
		defaultTimeout = uint64(600_000_000_000) // PFM default: 10 minutes
		fixedErrorAck  = `{"error":"ABCI code: 11151: error handling packet: see events for details"}`
	)
	testCases := []struct {
		timeout string // JSON value of the timeout field, empty to omit it
		// wantTimeout is the forwarded packet timeout relative to the block
		// time. It is zero when the packet must not be forwarded.
		wantTimeout uint64
	}{
		{"", defaultTimeout},
		{`"10m"`, defaultTimeout},
		{`"1h"`, 3_600_000_000_000},
		{"600000000000", defaultTimeout},
		{"1.5", 1},
		{"0", defaultTimeout},
		{"-5", defaultTimeout},
		{"-9223372036854775808", defaultTimeout}, // -2^63 is in range
		{"9223372036854774784", 9223372036854774784},
		{"9223372036854775807", 0}, // parses to 2^63, out of range
		{"9223372036854775808", 0},
		{"9.3e18", 0},
		{"1e19", 0},
		{"-1e19", 0},
		{"-9223372036854777856", 0}, // next float64 below -2^63
		{`"1e19"`, 0},               // invalid duration string
	}

	coordinator, celestia, chainA, chainB := SetupTest(t)
	pathA := ibctesting.NewTransferPath(chainA, celestia)
	pathB := ibctesting.NewTransferPath(celestia, chainB)
	coordinator.Setup(pathA)
	coordinator.Setup(pathB)
	celestiaApp, ok := celestia.App.(*app.App)
	require.True(t, ok)
	sender := chainA.SenderAccount.GetAddress().String()
	receiver := chainB.SenderAccount.GetAddress().String()

	for _, tc := range testCases {
		t.Run(tc.timeout, func(t *testing.T) {
			field := ""
			if tc.timeout != "" {
				field = `,"timeout":` + tc.timeout
			}
			memo := fmt.Sprintf(`{"forward":{"receiver":%q,"port":"transfer","channel":%q%s}}`,
				receiver, pathB.EndpointA.ChannelID, field)
			msg := transfertypes.NewMsgTransfer(pathA.EndpointA.ChannelConfig.PortID, pathA.EndpointA.ChannelID,
				sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1000)), sender, "unused",
				clienttypes.NewHeight(1, 100000), 0, memo)
			res, err := chainA.SendMsgs(msg)
			require.NoError(t, err)
			packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
			require.NoError(t, err)
			require.NoError(t, pathA.EndpointB.UpdateClient())
			blockTime := uint64(celestia.CurrentHeader.Time.UnixNano())
			recvRes, err := pathA.EndpointB.RecvPacketWithResult(packet)
			require.NoError(t, err)

			ack, ackErr := ibctesting.ParseAckFromEvents(recvRes.GetEvents())
			if tc.wantTimeout == 0 {
				require.NoError(t, ackErr, "packet must not be forwarded")
				require.Equal(t, fixedErrorAck, string(ack))
				return
			}
			require.Error(t, ackErr, "packet must be forwarded: ack %s", ack)
			fwd, err := ibctesting.ParsePacketFromEvents(recvRes.GetEvents())
			require.NoError(t, err)
			require.Equal(t, tc.wantTimeout, fwd.TimeoutTimestamp-blockTime)

			ctx := celestia.GetContext()
			bz := ctx.KVStore(celestiaApp.GetKey(pfmtypes.StoreKey)).Get(
				pfmtypes.RefundPacketKey(fwd.SourceChannel, fwd.SourcePort, fwd.Sequence))
			var inFlight pfmtypes.InFlightPacket
			require.NoError(t, celestiaApp.AppCodec().Unmarshal(bz, &inFlight))
			require.Equal(t, tc.wantTimeout, inFlight.Timeout)
		})
	}
}

// TestPFMForwardMemoWithoutMetadata checks memos with a "forward" key whose
// forward metadata decodes to nil. Such a memo must get the fixed
// packet-forward-middleware error acknowledgement, so the receive succeeds.
// A memo that packet-forward-middleware treats as a plain transfer must still
// be received as one.
func TestPFMForwardMemoWithoutMetadata(t *testing.T) {
	const fixedErrorAck = `{"error":"ABCI code: 11151: error handling packet: see events for details"}`
	successAck := string(channeltypes.NewResultAcknowledgement([]byte{byte(1)}).Acknowledgement())

	coordinator, celestia, chainA, chainB := SetupTest(t)
	pathA := ibctesting.NewTransferPath(chainA, celestia)
	pathB := ibctesting.NewTransferPath(celestia, chainB)
	coordinator.Setup(pathA)
	coordinator.Setup(pathB)
	forward := fmt.Sprintf(`{"receiver":%q,"port":"transfer","channel":%q}`,
		chainB.SenderAccount.GetAddress().String(), pathB.EndpointA.ChannelID)

	testCases := []struct {
		name    string
		memo    string
		wantAck string
	}{
		{"Forward null after forward", `{"forward":` + forward + `,"Forward":null}`, fixedErrorAck},
		{"FORWARD null after forward", `{"forward":` + forward + `,"FORWARD":null}`, fixedErrorAck},
		{"Forward object then forward null", `{"Forward":` + forward + `,"forward":null}`, successAck},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := transfertypes.NewMsgTransfer(pathA.EndpointA.ChannelConfig.PortID, pathA.EndpointA.ChannelID,
				sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1000)), chainA.SenderAccount.GetAddress().String(),
				celestia.SenderAccount.GetAddress().String(), clienttypes.NewHeight(1, 100000), 0, tc.memo)
			res, err := chainA.SendMsgs(msg)
			require.NoError(t, err)
			packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
			require.NoError(t, err)
			require.NoError(t, pathA.EndpointB.UpdateClient())
			recvRes, err := pathA.EndpointB.RecvPacketWithResult(packet)
			require.NoError(t, err, "the receive must succeed")
			ack, err := ibctesting.ParseAckFromEvents(recvRes.GetEvents())
			require.NoError(t, err)
			require.Equal(t, tc.wantAck, string(ack))
		})
	}
}

// TestPFMForwardMemoDepth checks the committed result of forward memos at
// and past the nesting depth limit, with an object or a string next, with a
// repeated key, and of an honest 2-hop memo whose next value is an object. A memo that nests too deep gets the fixed
// packet-forward-middleware error acknowledgement and is not forwarded.
func TestPFMForwardMemoDepth(t *testing.T) {
	const fixedErrorAck = `{"error":"ABCI code: 11151: error handling packet: see events for details"}`
	const maxDepth = 16 // app.maxPFMForwardMemoDepth

	coordinator, celestia, chainA, chainB := SetupTest(t)
	pathA := ibctesting.NewTransferPath(chainA, celestia)
	pathB := ibctesting.NewTransferPath(celestia, chainB)
	coordinator.Setup(pathA)
	coordinator.Setup(pathB)
	receiver := chainB.SenderAccount.GetAddress().String()

	// nested returns a next value that adds depth levels to the memo.
	nested := func(depth int) string {
		return strings.Repeat(`{"a":`, depth) + `1` + strings.Repeat(`}`, depth)
	}
	secondHop := `{"forward":{"receiver":"r2","port":"transfer","channel":"channel-9"}}`
	testCases := []struct {
		name     string
		next     string
		wantNext string // memo of the forwarded packet; empty when rejected
	}{
		{"2-hop", secondHop, secondHop},
		{"depth at limit", nested(maxDepth - 2), nested(maxDepth - 2)},
		{"depth over limit", nested(maxDepth - 1), ""},
		{"string next at depth limit", strconv.Quote(nested(maxDepth)), nested(maxDepth)},
		{"string next over depth limit", strconv.Quote(nested(maxDepth + 1)), ""},
		{"repeated key", `{"a":1,"a":2}`, ""},
		// packet-forward-middleware cannot encode a string next that is not
		// JSON, so it returns an error acknowledgement.
		{"string next not JSON", strconv.Quote("hello"), ""},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			memo := fmt.Sprintf(`{"forward":{"receiver":%q,"port":"transfer","channel":%q,"next":%s}}`,
				receiver, pathB.EndpointA.ChannelID, tc.next)
			msg := transfertypes.NewMsgTransfer(pathA.EndpointA.ChannelConfig.PortID, pathA.EndpointA.ChannelID,
				sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1000)), chainA.SenderAccount.GetAddress().String(), "unused",
				clienttypes.NewHeight(1, 100000), 0, memo)
			res, err := chainA.SendMsgs(msg)
			require.NoError(t, err)
			packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
			require.NoError(t, err)
			require.NoError(t, pathA.EndpointB.UpdateClient())
			recvRes, err := pathA.EndpointB.RecvPacketWithResult(packet)
			require.NoError(t, err)

			ack, ackErr := ibctesting.ParseAckFromEvents(recvRes.GetEvents())
			if tc.wantNext == "" {
				require.NoError(t, ackErr, "packet must not be forwarded")
				require.Equal(t, fixedErrorAck, string(ack))
				return
			}
			require.Error(t, ackErr, "packet must be forwarded: ack %s", ack)
			fwd, err := ibctesting.ParsePacketFromEvents(recvRes.GetEvents())
			require.NoError(t, err)
			var data transfertypes.FungibleTokenPacketData
			require.NoError(t, transfertypes.ModuleCdc.UnmarshalJSON(fwd.GetData(), &data))
			require.Equal(t, tc.wantNext, data.Memo)
		})
	}
}

// TestPFMForwardRetryWithStringNext checks the retry path of
// packet-forward-middleware for a string next at the depth limit: the
// forwarded packet times out, and the retried packet carries the same memo.
func TestPFMForwardRetryWithStringNext(t *testing.T) {
	coordinator, celestia, chainA, chainB := SetupTest(t)
	pathA := ibctesting.NewTransferPath(chainA, celestia)
	pathB := ibctesting.NewTransferPath(celestia, chainB)
	coordinator.Setup(pathA)
	coordinator.Setup(pathB)

	next := strings.Repeat(`{"a":`, 15) + `{}` + strings.Repeat(`}`, 15) // 16 levels, the limit
	memo := fmt.Sprintf(`{"forward":{"receiver":%q,"port":"transfer","channel":%q,"timeout":"1s","retries":1,"next":%s}}`,
		chainB.SenderAccount.GetAddress().String(), pathB.EndpointA.ChannelID, strconv.Quote(next))
	msg := transfertypes.NewMsgTransfer(pathA.EndpointA.ChannelConfig.PortID, pathA.EndpointA.ChannelID,
		sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1000)), chainA.SenderAccount.GetAddress().String(), "unused",
		clienttypes.NewHeight(1, 100000), 0, memo)
	res, err := chainA.SendMsgs(msg)
	require.NoError(t, err)
	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
	require.NoError(t, err)
	require.NoError(t, pathA.EndpointB.UpdateClient())
	recvRes, err := pathA.EndpointB.RecvPacketWithResult(packet)
	require.NoError(t, err)
	fwd, err := ibctesting.ParsePacketFromEvents(recvRes.GetEvents())
	require.NoError(t, err)

	// The forwarded packet times out and celestia sends it again.
	coordinator.IncrementTimeBy(time.Minute)
	coordinator.CommitBlock(chainB)
	require.NoError(t, pathB.EndpointA.UpdateClient())
	proof, proofHeight := pathB.EndpointB.QueryProof(host.PacketReceiptKey(fwd.DestinationPort, fwd.DestinationChannel, fwd.Sequence))
	nextSeqRecv, found := chainB.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceRecv(chainB.GetContext(), fwd.DestinationPort, fwd.DestinationChannel)
	require.True(t, found)
	timeoutMsg := channeltypes.NewMsgTimeout(fwd, nextSeqRecv, proof, proofHeight, celestia.SenderAccount.GetAddress().String())
	timeoutRes, err := celestia.SendMsgs(timeoutMsg)
	require.NoError(t, err)
	retried, err := ibctesting.ParsePacketFromEvents(timeoutRes.GetEvents())
	require.NoError(t, err, "celestia must retry the forwarded packet")
	require.Equal(t, fwd.Sequence+1, retried.Sequence)
	var data transfertypes.FungibleTokenPacketData
	require.NoError(t, transfertypes.ModuleCdc.UnmarshalJSON(retried.GetData(), &data))
	require.Equal(t, next, data.Memo)
}
