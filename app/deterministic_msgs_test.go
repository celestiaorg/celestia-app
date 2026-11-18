package app

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// forwardMemo returns a forward memo with a valid receiver, port and channel followed by extra.
func forwardMemo(extra string) string {
	return `{"forward":{"receiver":"cosmos1receiver","port":"transfer","channel":"channel-7"` + extra + `}}`
}

// nested returns a JSON object nested depth levels deep.
func nested(depth int) string {
	return strings.Repeat(`{"a":`, depth) + `1` + strings.Repeat(`}`, depth)
}

func TestValidateForwardMemoAllows(t *testing.T) {
	testCases := []struct {
		name string
		memo string
	}{
		{"empty memo", ``},
		{"plain text", `hello`},
		{"invalid JSON", `{"forward":{"receiver":"x"`},
		{"JSON array", `[{"forward":{}}]`},
		{"no forward key", `{"wasm":{"contract":"c","msg":{}}}`},
		{"forward null", `{"forward":null}`},
		{"single hop", forwardMemo(``)},
		{"string timeout", forwardMemo(`,"timeout":"10m"`)},
		{"numeric timeout", forwardMemo(`,"timeout":600000000000`)},
		{"exponent timeout", forwardMemo(`,"timeout":6e11`)},
		{"zero timeout", forwardMemo(`,"timeout":0`)},
		{"negative timeout", forwardMemo(`,"timeout":-1`)},
		{"largest timeout below 2^63", forwardMemo(`,"timeout":9223372036854774784`)},
		{"timeout -2^63", forwardMemo(`,"timeout":-9223372036854775808`)},
		{"retries", forwardMemo(`,"retries":2`)},
		{"retries max", forwardMemo(`,"retries":255`)},
		{"timeout and retries", forwardMemo(`,"timeout":"1h30m","retries":0`)},
		{"multi-hop next object", forwardMemo(`,"next":{"forward":{"receiver":"osmo1r","port":"transfer","channel":"channel-2","timeout":"5m","next":{"forward":{"receiver":"noble1r","port":"transfer","channel":"channel-9"}}}}`)},
		{"next escaped string", forwardMemo(`,"next":"{\"forward\":{\"receiver\":\"osmo1r\",\"port\":\"transfer\",\"channel\":\"channel-2\"}}"`)},
		{"next wasm hook", forwardMemo(`,"next":{"wasm":{"contract":"osmo1c","msg":{"swap":{"min":"1"}}}}`)},
		{"channel without prefix", `{"forward":{"receiver":"x","port":"transfer","channel":"notachannel"}}`},
		{"unknown forward key", forwardMemo(`,"memo":"x"`)},
		{"other top-level keys", `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0"},"wasm":{}}`},
		{"retries null", forwardMemo(`,"retries":null`)},
		{"next null", forwardMemo(`,"next":null`)},
		{"escaped receiver", `{"forward":{"receiver":"x\ty\/z\"\\","port":"transfer","channel":"channel-7"}}`},
		{"escaped timeout", forwardMemo(`,"timeout":"1\u006d"`)},
		{"escaped field name", `{"forward":{"receiver":"x","\u0070ort":"transfer","channel":"channel-7"}}`},
		{"escaped forward key", `{"\u0066orward":{"receiver":"x","port":"transfer","channel":"channel-7"}}`},
		{"paired surrogate", `{"forward":{"receiver":"\ud83d\ude00","port":"transfer","channel":"channel-7"}}`},
		{"paired surrogate in escaped next", forwardMemo(`,"next":"{\"a\":\"\\ud83d\\ude00\"}"`)},
		{"forward case variant only", `{"Forward":{"receiver":"x","port":"transfer","channel":"channel-7"}}`},
		{"forward fold variant only", `{"for_ward":{"receiver":"x","port":"transfer","channel":"channel-7"}}`},
		{"forward null with variant", `{"forward":null,"Forward":{"receiver":"x","port":"a/b","channel":"channel-7"}}`},
		{"next at depth limit", forwardMemo(`,"next":` + nested(maxPFMForwardMemoDepth-2))},
		{"deep memo without forward", nested(10 * maxPFMForwardMemoDepth)},
		{"same key in sibling objects in next", forwardMemo(`,"next":{"x":{"a":1},"y":{"a":1}}`)},
		{"keys differ in case inside next", forwardMemo(`,"next":{"a":1,"A":2}`)},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, validateForwardMemo(tc.memo))
		})
	}
}

func TestValidateForwardMemoRejects(t *testing.T) {
	testCases := []struct {
		name string
		memo string
	}{
		{"receiver empty", `{"forward":{"receiver":"","port":"transfer","channel":"channel-7"}}`},
		{"receiver missing", `{"forward":{"port":"transfer","channel":"channel-7"}}`},
		{"port blank", `{"forward":{"receiver":"x","port":"","channel":"channel-7"}}`},
		{"port slash", `{"forward":{"receiver":"x","port":"a/b","channel":"channel-7"}}`},
		{"port short", `{"forward":{"receiver":"x","port":"a","channel":"channel-7"}}`},
		{"port bad chars", `{"forward":{"receiver":"x","port":"tr@nsfer","channel":"channel-7"}}`},
		{"port huge", `{"forward":{"receiver":"x","port":"` + strings.Repeat("p", 200) + `","channel":"channel-7"}}`},
		{"channel blank", `{"forward":{"receiver":"x","port":"transfer","channel":""}}`},
		{"channel slash", `{"forward":{"receiver":"x","port":"transfer","channel":"channel/1"}}`},
		{"forward number", `{"forward":5}`},
		{"forward array", `{"forward":[]}`},
		{"forward string", `{"forward":"abc"}`},
		{"receiver number", `{"forward":{"receiver":5,"port":"transfer","channel":"channel-7"}}`},
		{"port bool", `{"forward":{"receiver":"x","port":true,"channel":"channel-7"}}`},
		{"channel null", `{"forward":{"receiver":"x","port":"transfer","channel":null}}`},
		{"retries 300", forwardMemo(`,"retries":300`)},
		{"retries negative", forwardMemo(`,"retries":-1`)},
		{"retries fraction", forwardMemo(`,"retries":1.5`)},
		{"retries exponent", forwardMemo(`,"retries":1e0`)},
		{"retries string", forwardMemo(`,"retries":"1"`)},
		{"timeout bad string", forwardMemo(`,"timeout":"abc"`)},
		{"timeout overflow string", forwardMemo(`,"timeout":"9999999999h"`)},
		{"timeout bool", forwardMemo(`,"timeout":true`)},
		{"timeout object", forwardMemo(`,"timeout":{}`)},
		{"timeout null", forwardMemo(`,"timeout":null`)},
		{"timeout 1e400", forwardMemo(`,"timeout":1e400`)},
		{"timeout 1e19", forwardMemo(`,"timeout":1e19`)},
		{"timeout 9.3e18", forwardMemo(`,"timeout":9.3e18`)},
		{"timeout -1e19", forwardMemo(`,"timeout":-1e19`)},
		{"timeout MaxInt64", forwardMemo(`,"timeout":9223372036854775807`)},
		{"timeout 2^63", forwardMemo(`,"timeout":9223372036854775808`)},
		{"timeout 2^63 float", forwardMemo(`,"timeout":9.223372036854775808e18`)},
		{"timeout below -2^63", forwardMemo(`,"timeout":-9223372036854777856`)},
		{"timeout fraction", forwardMemo(`,"timeout":1.5`)},
		{"next bad primitive", forwardMemo(`,"next":"abc"`)},
		{"next bad escaped JSON", forwardMemo(`,"next":"{\"a\":"`)},
		// Shapes whose decoding depends on JSON library details.
		{"duplicate forward", `{"forward":{"receiver":"x","port":"transfer","channel":"channel-7"},"forward":{"receiver":"x","port":"transfer","channel":"channel-7"}}`},
		{"forward with null variant", `{"forward":{"receiver":"x","port":"transfer","channel":"channel-7"},"FORWARD":null}`},
		{"duplicate port", `{"forward":{"receiver":"x","port":"x/y","port":"transfer","channel":"channel-7"}}`},
		{"port case variant", `{"forward":{"receiver":"x","Port":"transfer","channel":"channel-7"}}`},
		{"retries fold variant", "{\"forward\":{\"receiver\":\"x\",\"port\":\"transfer\",\"channel\":\"channel-7\",\"retrieſ\":1}}"},
		{"timeout underscore variant", forwardMemo(`,"time_out":"1m"`)},
		{"lone surrogate", `{"forward":{"receiver":"x","port":"transfer","channel":"channel-7","next":{"a":"\ud800"}}}`},
		{"invalid UTF-8", "{\"forward\":{\"receiver\":\"\xff\",\"port\":\"transfer\",\"channel\":\"channel-7\"}}"},
		{"too long", forwardMemo(`,"next":{"a":"` + strings.Repeat("a", transfertypes.MaximumMemoLength) + `"}`)},
		{"next empty string", forwardMemo(`,"next":""`)},
		{"next too deep", forwardMemo(`,"next":` + nested(maxPFMForwardMemoDepth-1))},
		{"next array too deep", forwardMemo(`,"next":` + strings.Repeat("[", maxPFMForwardMemoDepth) + strings.Repeat("]", maxPFMForwardMemoDepth))},
		{"too deep and invalid", `{"forward":{"receiver":"x","port":"transfer","channel":"channel-7","next":` + strings.Repeat(`{"a":`, 20000)},
		{"forward with case variant", `{"forward":{"receiver":"x","port":"transfer","channel":"channel-7"},"Forward":{}}`},
		{"case variant before forward", `{"Forward":{},"forward":{"receiver":"x","port":"transfer","channel":"channel-7"}}`},
		{"forward with null upper-case variant", `{"forward":{"receiver":"x","port":"transfer","channel":"channel-7"},"FORWARD":null}`},
		{"next with case variant", forwardMemo(`,"Next":{"a":1},"next":{"b":2}`)},
		{"lone low surrogate", `{"forward":{"receiver":"\udc00","port":"transfer","channel":"channel-7"}}`},
		{"reversed surrogate pair", `{"forward":{"receiver":"\ude00\ud83d","port":"transfer","channel":"channel-7"}}`},
		{"lone surrogate in escaped next", forwardMemo(`,"next":"{\"a\":\"\\ud800\"}"`)},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, validateForwardMemo(tc.memo))
		})
	}
}

func TestValidateDeterministicMsgs(t *testing.T) {
	addr := sdk.AccAddress("addr________________")
	recv := func(memo string) sdk.Msg {
		data := transfertypes.NewFungibleTokenPacketData("utia", "1", "sender", "receiver", memo)
		packet := channeltypes.NewPacket(data.GetBytes(), 1, "transfer", "channel-0", "transfer", "channel-1", channeltypes.Packet{}.TimeoutHeight, 1)
		return channeltypes.NewMsgRecvPacket(packet, []byte("proof"), channeltypes.Packet{}.TimeoutHeight, addr.String())
	}
	recvRaw := func(data []byte, port string) sdk.Msg {
		packet := channeltypes.NewPacket(data, 1, port, "channel-0", port, "channel-1", channeltypes.Packet{}.TimeoutHeight, 1)
		return channeltypes.NewMsgRecvPacket(packet, []byte("proof"), channeltypes.Packet{}.TimeoutHeight, addr.String())
	}
	badData := transfertypes.NewFungibleTokenPacketData("utia", "1", "sender", "receiver", forwardMemo(`,"timeout":1e19`)).GetBytes()
	ics20 := func(extra string) []byte {
		return []byte(`{"amount":"1","denom":"utia","receiver":"r","sender":"s"` + extra + `}`)
	}
	exec := func(msg sdk.Msg) sdk.Msg {
		m := authz.NewMsgExec(addr, []sdk.Msg{msg})
		return &m
	}
	proposal := func(msg sdk.Msg) sdk.Msg {
		m, err := govv1.NewMsgSubmitProposal([]sdk.Msg{msg}, nil, addr.String(), "", "title", "summary", false)
		require.NoError(t, err)
		return m
	}
	govParams := func(maxDepositPeriod *time.Duration) sdk.Msg {
		params := govv1.DefaultParams()
		params.MaxDepositPeriod = maxDepositPeriod
		return &govv1.MsgUpdateParams{Authority: authtypes.NewModuleAddress("gov").String(), Params: params}
	}
	dur := func(d time.Duration) *time.Duration { return &d }

	good := recv(forwardMemo(`,"timeout":"10m"`))
	bad := recv(forwardMemo(`,"timeout":1e19`))
	send := banktypes.NewMsgSend(addr, addr, nil)

	testCases := []struct {
		name    string
		msgs    []sdk.Msg
		wantErr bool
	}{
		{"no packets", []sdk.Msg{send}, false},
		{"valid forward", []sdk.Msg{good}, false},
		{"non-forward memo", []sdk.Msg{recv(`{"wasm":{}}`)}, false},
		{"bad forward to another port", []sdk.Msg{recvRaw(badData, "icahost")}, false},
		{"bad forward to transfer port", []sdk.Msg{recvRaw(badData, transfertypes.PortID)}, true},
		{"packet data at size limit", []sdk.Msg{recvRaw(ics20(`,"memo":"`+strings.Repeat("a", maxTransferPacketDataSize-len(ics20(`,"memo":""`)))+`"`), transfertypes.PortID)}, false},
		{"packet data over size limit", []sdk.Msg{recvRaw(ics20(`,"memo":"`+strings.Repeat("a", maxTransferPacketDataSize)+`"`), transfertypes.PortID)}, true},
		{"packet data over size limit to another port", []sdk.Msg{recvRaw(ics20(`,"memo":"`+strings.Repeat("a", maxTransferPacketDataSize)+`"`), "icahost")}, false},
		{"packet data with escaped field name", []sdk.Msg{recvRaw(ics20(`,"\u006demo":"x"`), transfertypes.PortID)}, false},
		{"packet data with unknown key", []sdk.Msg{recvRaw(ics20(`,"forward":5`), transfertypes.PortID)}, true},
		{"packet data with case variant key", []sdk.Msg{recvRaw(ics20(`,"Memo":"x"`), transfertypes.PortID)}, true},
		{"packet data with duplicate memo", []sdk.Msg{recvRaw(ics20(`,"memo":"","memo":"x"`), transfertypes.PortID)}, true},
		{"packet data not an object", []sdk.Msg{recvRaw([]byte(`[1]`), transfertypes.PortID)}, false},
		{"packet data null", []sdk.Msg{recvRaw([]byte(`null`), transfertypes.PortID)}, false},
		{"bad forward", []sdk.Msg{send, bad}, true},
		{"bad forward after good", []sdk.Msg{good, bad}, true},
		{"bad forward in MsgExec", []sdk.Msg{exec(bad)}, true},
		{"good forward in MsgExec", []sdk.Msg{exec(good)}, false},
		{"bad forward in MsgSubmitProposal", []sdk.Msg{proposal(bad)}, true},
		{"gov params default", []sdk.Msg{proposal(govParams(dur(time.Hour)))}, false},
		{"gov params nil max deposit period", []sdk.Msg{proposal(govParams(nil))}, false},
		{"gov params sub-second max deposit period", []sdk.Msg{proposal(govParams(dur(time.Millisecond)))}, false},
		{"gov params zero max deposit period", []sdk.Msg{proposal(govParams(dur(0)))}, true},
		{"gov params negative max deposit period", []sdk.Msg{proposal(govParams(dur(-time.Second)))}, true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDeterministicMsgs(msgsTx{msgs: tc.msgs})
			if tc.wantErr {
				require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateDeterministicMsgsNamesPacket(t *testing.T) {
	data := transfertypes.NewFungibleTokenPacketData("utia", "1", "sender", "receiver", forwardMemo(`,"timeout":1e19`))
	packet := channeltypes.NewPacket(data.GetBytes(), 42, "transfer", "channel-3", "transfer", "channel-9", channeltypes.Packet{}.TimeoutHeight, 1)
	msg := channeltypes.NewMsgRecvPacket(packet, []byte("proof"), channeltypes.Packet{}.TimeoutHeight, sdk.AccAddress("addr________________").String())
	err := validateDeterministicMsgs(msgsTx{msgs: []sdk.Msg{msg}})
	require.ErrorContains(t, err, `packet 42 from "transfer"/"channel-3" to "transfer"/"channel-9"`)

	msg.Packet.SourcePort = "%s\xff\n" + strings.Repeat("A", 1<<20)
	err = validateDeterministicMsgs(msgsTx{msgs: []sdk.Msg{msg}})
	require.Error(t, err)
	require.Less(t, len(err.Error()), 1000)
	require.True(t, utf8.ValidString(err.Error()))
	require.NotContains(t, err.Error(), "\n")
}
