package app

import (
	"encoding/json"
	"fmt"
	"math"
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

// forwardMemo returns a forward memo with the given extra fields after a valid
// receiver, port and channel.
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
		// Validate() failures.
		{"receiver empty", `{"forward":{"receiver":"","port":"transfer","channel":"channel-7"}}`},
		{"receiver missing", `{"forward":{"port":"transfer","channel":"channel-7"}}`},
		{"port blank", `{"forward":{"receiver":"x","port":"","channel":"channel-7"}}`},
		{"port slash", `{"forward":{"receiver":"x","port":"a/b","channel":"channel-7"}}`},
		{"port short", `{"forward":{"receiver":"x","port":"a","channel":"channel-7"}}`},
		{"port bad chars", `{"forward":{"receiver":"x","port":"tr@nsfer","channel":"channel-7"}}`},
		{"port huge", `{"forward":{"receiver":"x","port":"` + strings.Repeat("p", 200) + `","channel":"channel-7"}}`},
		{"channel blank", `{"forward":{"receiver":"x","port":"transfer","channel":""}}`},
		{"channel slash", `{"forward":{"receiver":"x","port":"transfer","channel":"channel/1"}}`},
		// JSON type errors.
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
		// Numeric timeouts outside int64.
		{"timeout 1e19", forwardMemo(`,"timeout":1e19`)},
		{"timeout 9.3e18", forwardMemo(`,"timeout":9.3e18`)},
		{"timeout -1e19", forwardMemo(`,"timeout":-1e19`)},
		{"timeout MaxInt64", forwardMemo(`,"timeout":9223372036854775807`)},
		{"timeout 2^63", forwardMemo(`,"timeout":9223372036854775808`)},
		{"timeout 2^63 float", forwardMemo(`,"timeout":9.223372036854775808e18`)},
		{"timeout below -2^63", forwardMemo(`,"timeout":-9223372036854777856`)},
		{"timeout fraction", forwardMemo(`,"timeout":1.5`)},
		// next that cannot be re-encoded.
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
		{"escaped next too deep", forwardMemo(`,"next":"` + strings.Repeat("[", maxPFMForwardMemoDepth+1) + strings.Repeat("]", maxPFMForwardMemoDepth+1) + `"`)},
		// forward.next must be absent, null, an object, or a string. A string
		// next that is JSON must be an object within the same limits.
		{"next array", forwardMemo(`,"next":[1,{"a":1}]`)},
		{"next number", forwardMemo(`,"next":5`)},
		{"next escaped array", forwardMemo(`,"next":"[1,2]"`)},
		{"next escaped null", forwardMemo(`,"next":"null"`)},
		{"next escaped number", forwardMemo(`,"next":"5"`)},
		{"next escaped string", forwardMemo(`,"next":"\"{}\""`)},
		{"next escaped too deep object", forwardMemo(`,"next":"` + strings.Repeat(`{\"a\":`, maxPFMForwardMemoDepth+1) + `1` + strings.Repeat(`}`, maxPFMForwardMemoDepth+1) + `"`)},
		// No object may repeat a key.
		{"next duplicate keys", forwardMemo(`,"next":{"a":1,"a":2}`)},
		{"next duplicate nested keys", forwardMemo(`,"next":{"x":[{"a":1,"a":2}]}`)},
		{"escaped next duplicate keys", forwardMemo(`,"next":"{\"a\":1,\"a\":2}"`)},
		{"duplicate next", forwardMemo(`,"next":{"a":1},"next":{}`)},
		{"duplicate top-level key", forwardMemo(``)[:len(forwardMemo(``))-1] + `,"a":1,"a":2}`},
		{"duplicate top-level key, other case", forwardMemo(``)[:len(forwardMemo(``))-1] + `,"a":1,"A":2}`},
		{"duplicate key in other top-level value", forwardMemo(``)[:len(forwardMemo(``))-1] + `,"other":{"a":1,"a":2}}`},
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

// TestValidateDeterministicMsgsCost checks that the filter cost stays linear in
// the tx size for worst-case packets in an 8 MiB tx.
func TestValidateDeterministicMsgsCost(t *testing.T) {
	const txSize = 8 << 20
	addr := sdk.AccAddress("addr________________").String()
	recvData := func(data []byte, port string) *channeltypes.MsgRecvPacket {
		packet := channeltypes.NewPacket(data, 1, port, "channel-0", port, "channel-1", channeltypes.Packet{}.TimeoutHeight, 1)
		return channeltypes.NewMsgRecvPacket(packet, []byte("proof"), channeltypes.Packet{}.TimeoutHeight, addr)
	}
	recv := func(memo string) *channeltypes.MsgRecvPacket {
		return recvData(transfertypes.NewFungibleTokenPacketData("utia", "1", "sender", "receiver", memo).GetBytes(), transfertypes.PortID)
	}
	// fill returns filler repeated between prefix and suffix up to about size bytes.
	fill := func(prefix, filler, suffix string, size int) string {
		return prefix + strings.Repeat(filler, (size-len(prefix)-len(suffix))/len(filler)) + suffix
	}
	// deep returns a memo nested with open and closing up to about size bytes.
	deep := func(prefix, open, closing, suffix string, size int) string {
		n := (size - len(prefix) - len(suffix) - 1) / (len(open) + len(closing))
		return prefix + strings.Repeat(open, n) + `1` + strings.Repeat(closing, n) + suffix
	}
	// repeated returns enough copies of msg to fill the tx.
	repeated := func(msg *channeltypes.MsgRecvPacket) []sdk.Msg {
		msgs := make([]sdk.Msg, txSize/msg.Size())
		for i := range msgs {
			msgs[i] = msg
		}
		return msgs
	}
	// distinctKeys returns ICS-20 packet data of about size bytes with distinct unknown keys.
	distinctKeys := func(size int) []byte {
		var sb strings.Builder
		sb.WriteString(`{`)
		for i := 0; sb.Len() < size-64; i++ {
			fmt.Fprintf(&sb, `"%x":1,`, i)
		}
		sb.WriteString(`"amount":"1","denom":"utia","receiver":"r","sender":"s","memo":""}`)
		return []byte(sb.String())
	}
	fwdPrefix := `{"forward":{"receiver":"r","port":"transfer","channel":"channel-7","next":`
	maxMemo := transfertypes.MaximumMemoLength
	// distinctObject returns a JSON object of about size bytes with distinct keys.
	distinctObject := func(size int) string {
		var sb strings.Builder
		sb.WriteString(`{"":1`)
		for i := 0; sb.Len() < size-16; i++ {
			fmt.Fprintf(&sb, `,"%x":1`, i)
		}
		sb.WriteString(`}`)
		return sb.String()
	}
	deepNext := fwdPrefix + strings.Repeat(`{"a":`, maxPFMForwardMemoDepth-3) + distinctObject(maxMemo-len(fwdPrefix)-6*maxPFMForwardMemoDepth) + strings.Repeat(`}`, maxPFMForwardMemoDepth-3) + `}}`
	require.LessOrEqual(t, len(deepNext), maxMemo)
	require.NoError(t, validateForwardMemo(deepNext))
	deepStringNext := fwdPrefix + jsonString(strings.Repeat(`{"a":`, maxPFMForwardMemoDepth-1)+distinctObject(maxMemo*3/4-len(fwdPrefix)-12*maxPFMForwardMemoDepth)+strings.Repeat(`}`, maxPFMForwardMemoDepth-1)) + `}}`
	require.LessOrEqual(t, len(deepStringNext), maxMemo)
	require.NoError(t, validateForwardMemo(deepStringNext))
	emptyObjects := fwdPrefix + fill(`{"a":[{}`, `,{}`, `]}`, maxMemo-len(fwdPrefix)-2) + `}}`
	require.LessOrEqual(t, len(emptyObjects), maxMemo)
	require.NoError(t, validateForwardMemo(emptyObjects))
	// Each one-key object allocates a key set.
	oneKeyObjects := fwdPrefix + fill(`{"a":[{"a":0}`, `,{"a":0}`, `]}`, maxMemo-len(fwdPrefix)-2) + `}}`
	require.LessOrEqual(t, len(oneKeyObjects), maxMemo)
	require.Greater(t, len(oneKeyObjects), maxMemo-16)
	require.NoError(t, validateForwardMemo(oneKeyObjects))
	// recvMax returns a packet with the largest memo from build whose packet
	// data stays within the size limit.
	recvMax := func(build func(size int) string) *channeltypes.MsgRecvPacket {
		for size := maxTransferPacketDataSize; ; {
			msg := recv(build(size))
			if n := len(msg.Packet.Data); n <= maxTransferPacketDataSize {
				return msg
			} else {
				size = size*maxTransferPacketDataSize/n - 100
			}
		}
	}
	// tiny is the smallest MsgRecvPacket that carries a valid forward memo.
	tiny := &channeltypes.MsgRecvPacket{
		Packet: channeltypes.Packet{
			Sequence: 1, SourcePort: "p", SourceChannel: "c", DestinationPort: transfertypes.PortID, DestinationChannel: "c",
			Data: []byte(`{"amount":"1","denom":"u","memo":"{\"forward\":{\"receiver\":\"r\",\"port\":\"tr\",\"channel\":\"channel-0\"}}","receiver":"r","sender":"s"}`),
		},
		Signer: "s",
	}
	require.NoError(t, validateDeterministicMsgs(msgsTx{msgs: []sdk.Msg{tiny}}))
	// proposal returns a proposal that fills the tx with copies of msg.
	proposal := func(msg *channeltypes.MsgRecvPacket) []sdk.Msg {
		msgs := repeated(msg)
		p, err := govv1.NewMsgSubmitProposal(msgs, nil, addr, "", "title", "summary", false)
		require.NoError(t, err)
		p, err = govv1.NewMsgSubmitProposal(msgs[:len(msgs)*txSize/p.Size()], nil, addr, "", "title", "summary", false)
		require.NoError(t, err)
		return []sdk.Msg{p}
	}

	testCases := []struct {
		name    string
		msgs    []sdk.Msg
		wantErr bool
	}{
		{"max-size forwards at depth limit with distinct keys", repeated(recv(deepNext)), false},
		{"max-size forwards with string next at depth limit", repeated(recv(deepStringNext)), false},
		{"max-size forwards with many empty objects", repeated(recv(emptyObjects)), false},
		{"max-size forwards with many one-key objects", repeated(recv(oneKeyObjects)), false},
		{"max-size forwards with repeated next keys", repeated(recv(pfmRepeatedNextMemo())), true},
		{"max-size forwards with deep string next", repeated(recv(pfmDeepStringNextMemo(3000))), true},
		{"max-size forwards with long numeric timeout", repeated(recv(forwardMemo(`,"timeout":0.` + strings.Repeat("0", maxMemo-200)))), false},
		{"max-size forwards nested too deep", repeated(recv(deep(fwdPrefix, `{"a":`, `}`, `}}`, maxMemo))), true},
		{"max-size forwards with alternating nesting", repeated(recv(deep(fwdPrefix, `{"a":[`, `]}`, `}}`, maxMemo))), true},
		{"large deep memos without forward", repeated(recvMax(func(n int) string { return deep(``, `{"a":`, `}`, ``, n) })), false},
		{"large alternating memos without forward", repeated(recvMax(func(n int) string { return deep(``, `{"a":[`, `]}`, ``, n) })), false},
		{"large memos with many keys", repeated(recvMax(func(n int) string { return fill(`{`, `"a":1,`, `"b":2}`, n) })), false},
		{"large memos with many empty keys", repeated(recvMax(func(n int) string { return fill(`{`, `"":1,`, `"b":2}`, n) })), false},
		{"large memos with many keys and null forward", repeated(recvMax(func(n int) string { return fill(`{"forward":null,`, `"a":1,`, `"b":2}`, n) })), false},
		{"large memos with many escaped keys", repeated(recvMax(func(n int) string { return fill(`{`, `"\u0061":1,`, `"b":2}`, n) })), false},
		{"large string memos", repeated(recvMax(func(n int) string { return fill(`"`, `a`, `"`, n) })), false},
		{"large escaped string memos", repeated(recvMax(func(n int) string { return fill(`"`, `<`, `"`, n) })), false},
		{"large forward memos", repeated(recvMax(func(n int) string {
			return fill(`{"forward":{"receiver":"`, `a`, `","port":"transfer","channel":"channel-7"}}`, n)
		})), true},
		{"one 8 MiB packet", []sdk.Msg{recv(fill(`"`, `a`, `"`, txSize))}, true},
		{"distinct unknown keys on the transfer port", []sdk.Msg{recvData(distinctKeys(txSize), transfertypes.PortID)}, true},
		{"distinct unknown keys within the size limit", repeated(recvData(distinctKeys(maxTransferPacketDataSize-200), transfertypes.PortID)), true},
		{"distinct unknown keys on another port", []sdk.Msg{recvData(distinctKeys(txSize), "icahost")}, false},
		{"duplicate memo keys", repeated(recvData([]byte(fill(`{"amount":"1","denom":"utia","receiver":"r","sender":"s",`, `"memo":"",`, `"memo":""}`, maxTransferPacketDataSize)), transfertypes.PortID)), true},
		{"many small forwards in a proposal", proposal(recv(forwardMemo(``))), false},
		{"many tiny forwards in a proposal", proposal(tiny), false},
		{"many tiny forwards", repeated(tiny), false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var packets []*channeltypes.MsgRecvPacket
			for _, msg := range tc.msgs {
				if p, ok := msg.(*govv1.MsgSubmitProposal); ok {
					inner, err := p.GetMsgs()
					require.NoError(t, err)
					for _, m := range inner {
						packets = append(packets, m.(*channeltypes.MsgRecvPacket))
					}
					continue
				}
				packets = append(packets, msg.(*channeltypes.MsgRecvPacket))
			}
			size := 0
			for _, msg := range tc.msgs {
				size += msg.(interface{ Size() int }).Size()
			}
			require.Greater(t, size, txSize*9/10)
			require.LessOrEqual(t, size, txSize+txSize/10)

			// Decoding every packet's data is a baseline that scales with the
			// machine. The baseline and the filter are timed in turn, and the
			// best of up to costRuns runs of each is kept, so that load on a
			// shared runner, which slows down single runs, cancels out. The
			// loop stops as soon as the best times meet both bounds.
			baseline, elapsed := time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)
			for run := 0; run < costRuns; run++ {
				start := time.Now()
				for _, p := range packets {
					var data transfertypes.FungibleTokenPacketData
					_ = transfertypes.ModuleCdc.UnmarshalJSON(p.Packet.GetData(), &data)
				}
				baseline = min(baseline, time.Since(start))

				start = time.Now()
				err := validateDeterministicMsgs(msgsTx{msgs: tc.msgs})
				elapsed = min(elapsed, time.Since(start))
				if tc.wantErr {
					require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
				} else {
					require.NoError(t, err)
				}
				t.Logf("run %d: %d packets, %d bytes: filter took %s, decoding took %s (best so far)", run, len(packets), size, elapsed, baseline)
				if elapsed < 5*baseline+100*time.Millisecond && elapsed < maxFilterCost {
					break
				}
			}
			// The filter must cost at most a small multiple of decoding the
			// same packet data, so it is linear in the tx size.
			require.Less(t, elapsed, 5*baseline+100*time.Millisecond, "filter took %s, decoding took %s", elapsed, baseline)
			// A backstop against a slowdown that the baseline also suffers.
			require.Less(t, elapsed, maxFilterCost, "filter took %s", elapsed)
		})
	}
}

// The tests below are ported from the packet-forward memo limit tests on
// main (app/pfm_timeout_internal_test.go), so that this filter rejects every
// forward memo that main rejects in consensus.

// transferPacketData returns ICS-20 packet data that carries memo.
func transferPacketData(t *testing.T, memo string) []byte {
	t.Helper()
	data, err := json.Marshal(transfertypes.FungibleTokenPacketData{
		Denom: "uatom", Amount: "1", Sender: "remote", Receiver: "pfm", Memo: memo,
	})
	require.NoError(t, err)
	return data
}

// rejectPFMForwardMemo reports whether the filter rejects packet data.
func rejectPFMForwardMemo(packetData []byte) bool {
	return validateTransferPacketData(packetData) != nil
}

// nestedJSON returns a JSON object that nests depth levels.
func nestedJSON(depth int) string {
	return strings.Repeat(`{"a":`, depth-1) + `{}` + strings.Repeat(`}`, depth-1)
}

// pfmForwardMemo returns a forward memo whose JSON nesting depth is depth
// (at least 2) and whose length is at least size. Its next value is a chain
// of nested objects.
func pfmForwardMemo(depth, size int) string {
	next := ""
	if depth > 2 {
		next = nestedJSON(depth - 2)
	}
	return pfmForwardMemoWithNext(next, size)
}

// pfmForwardMemoWithNext returns a forward memo with the given JSON value of
// forward.next (empty to omit it), padded to at least size bytes.
func pfmForwardMemoWithNext(next string, size int) string {
	head := `{"forward":{"receiver":"`
	tail := `","port":"transfer","channel":"channel-0"`
	if next != "" {
		tail += `,"next":` + next
	}
	tail += `}}`
	pad := max(1, size-len(head)-len(tail))
	return head + strings.Repeat("r", pad) + tail
}

// jsonString returns s as a JSON string.
func jsonString(s string) string {
	bz, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(bz)
}

// pfmRepeatedNextMemo returns a forward memo of about 32 KB whose next key
// repeats: the first next holds 2000 distinct keys, and 1860 empty next
// objects follow it.
func pfmRepeatedNextMemo() string {
	var keys strings.Builder
	for i := range 2000 {
		if i > 0 {
			keys.WriteByte(',')
		}
		keys.WriteString(jsonString(string(rune('A'+i/64)) + string(rune('A'+i%64))))
		keys.WriteString(`:0`)
	}
	return `{"forward":{"next":{` + keys.String() + `}` + strings.Repeat(`,"next":{}`, 1860) + `}}`
}

// pfmDeepStringNextMemo returns a forward memo whose string next holds a JSON
// object that nests depth levels.
func pfmDeepStringNextMemo(depth int) string {
	return `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","timeout":"1ns","retries":255,"next":` + jsonString(nestedJSON(depth)) + `}}`
}

// TestRejectPFMForwardMemoLimits checks the bounds on the nesting depth and
// the length of a forward memo, at the limits and one past them.
func TestRejectPFMForwardMemoLimits(t *testing.T) {
	require.Len(t, pfmForwardMemo(2, transfertypes.MaximumMemoLength), transfertypes.MaximumMemoLength)

	testCases := []struct {
		name string
		memo string
		want bool
	}{
		{"depth 2", pfmForwardMemo(2, 0), false},
		{"depth at limit", pfmForwardMemo(maxPFMForwardMemoDepth, 0), false},
		{"depth over limit", pfmForwardMemo(maxPFMForwardMemoDepth+1, 0), true},
		{"depth 4000", pfmForwardMemo(4000, 0), true},
		{"array depth over limit", `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","next":` + strings.Repeat(`[`, maxPFMForwardMemoDepth-1) + strings.Repeat(`]`, maxPFMForwardMemoDepth-1) + `}}`, true},
		{"brackets in strings do not count", `{"forward":{"receiver":"` + strings.Repeat(`{[`, 100) + `\"{","port":"transfer","channel":"channel-0"}}`, false},
		{"length at limit", pfmForwardMemo(2, transfertypes.MaximumMemoLength), false},
		{"length over limit", pfmForwardMemo(2, transfertypes.MaximumMemoLength+1), true},
		{"length and depth at limit", pfmForwardMemo(maxPFMForwardMemoDepth, transfertypes.MaximumMemoLength), false},
		{"length over limit with invalid forward", `{"forward":{"receiver":"` + strings.Repeat("r", transfertypes.MaximumMemoLength) + `","retries":300}}`, true},
		// A string next is the memo of the forwarded packet.
		{"string next at depth limit", pfmForwardMemoWithNext(jsonString(nestedJSON(maxPFMForwardMemoDepth)), 0), false},
		{"string next over depth limit", pfmForwardMemoWithNext(jsonString(nestedJSON(maxPFMForwardMemoDepth+1)), 0), true},
		{"string next 3000 levels", pfmDeepStringNextMemo(3000), true},
		{"string next array over depth limit", pfmForwardMemoWithNext(jsonString(strings.Repeat(`[`, maxPFMForwardMemoDepth+1)+strings.Repeat(`]`, maxPFMForwardMemoDepth+1)), 0), true},
		{"string next with repeated key", pfmForwardMemoWithNext(jsonString(`{"a":1,"a":2}`), 0), true},
		{"string next with repeated nested key", pfmForwardMemoWithNext(jsonString(`{"a":{"b":1,"b":2}}`), 0), true},
		{"string next holding a JSON string", pfmForwardMemoWithNext(jsonString(jsonString(`{"a":1}`)), 0), true},
		// main accepts a string next that is not JSON; this filter rejects
		// it, because packet-forward-middleware cannot encode it.
		{"string next, not JSON", pfmForwardMemoWithNext(jsonString(`{"a":`), 0), true},
		{"string next, number", pfmForwardMemoWithNext(jsonString(`5`), 0), true},
		{"string next, null", pfmForwardMemoWithNext(jsonString(`null`), 0), true},
		{"string next, true", pfmForwardMemoWithNext(jsonString(`true`), 0), true},
		{"string next, array", pfmForwardMemoWithNext(jsonString(`[{"a":1}]`), 0), true},
		{"string next, object", pfmForwardMemoWithNext(jsonString(` {"a":[1]} `), 0), false},
		{"next null", pfmForwardMemoWithNext(`null`, 0), false},
		{"next number", pfmForwardMemoWithNext(`5`, 0), true},
		{"next true", pfmForwardMemoWithNext(`true`, 0), true},
		{"next array", pfmForwardMemoWithNext(`[{"a":1}]`, 0), true},
		{"Next key case, next array", `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","Next":[]}}`, true},
		{"next key in a nested object is not forward.next", `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","next":{"next":[1]}},"a":{"next":5}}`, false},
		{"Next key case, string next over depth limit", `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","NEXT":` + jsonString(nestedJSON(maxPFMForwardMemoDepth+1)) + `}}`, true},
		// Memos that packet-forward-middleware does not forward are not
		// bounded here.
		{"deep memo without forward", `{"a":` + strings.Repeat(`{"a":`, 4000) + `1` + strings.Repeat(`}`, 4001), false},
		{"long memo without forward", `{"a":"` + strings.Repeat("r", 2*transfertypes.MaximumMemoLength) + `"}`, false},
		{"repeated keys without forward", `{"a":1,"a":2,"forward":null}`, false},
		{"deep forward null", `{"forward":null,"a":` + strings.Repeat(`[`, 100) + strings.Repeat(`]`, 100) + `}`, false},
		// Honest multi-hop memos.
		{"1-hop", `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","timeout":"10m","retries":2}}`, false},
		{"2-hop", `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","next":{"forward":{"receiver":"r2","port":"transfer","channel":"channel-1"}}}}`, false},
		{"2-hop next string", `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","next":"{\"forward\":{\"receiver\":\"r2\",\"port\":\"transfer\",\"channel\":\"channel-1\"}}"}}`, false},
		// Amplified memos under the length limit.
		{"repeated next keys", pfmRepeatedNextMemo(), true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.LessOrEqual(t, len(transferPacketData(t, tc.memo)), maxTransferPacketDataSize)
			require.Equal(t, tc.want, rejectPFMForwardMemo(transferPacketData(t, tc.memo)))
		})
	}
	require.LessOrEqual(t, len(pfmRepeatedNextMemo()), transfertypes.MaximumMemoLength)
	require.Greater(t, len(pfmRepeatedNextMemo()), 30_000)
	require.LessOrEqual(t, len(pfmDeepStringNextMemo(3000)), transfertypes.MaximumMemoLength)
}

// TestRejectPFMForwardMemoRepeatedKeys checks that a forward memo with a
// repeated object key is rejected. encoding/json matches the keys of the
// memo and of the forward object to struct fields without case, so those
// compare without case. The keys inside next compare exactly, as
// packet-forward-middleware keeps them.
func TestRejectPFMForwardMemoRepeatedKeys(t *testing.T) {
	const fwd = `"receiver":"r","port":"transfer","channel":"channel-0"`
	testCases := []struct {
		name string
		memo string
		want bool
	}{
		{"no repeated keys", `{"forward":{` + fwd + `,"next":{"a":1,"b":{"a":2}}},"other":{"a":1}}`, false},
		{"case differs inside next", `{"forward":{` + fwd + `,"next":{"a":1,"A":2}}}`, false},
		{"case differs inside other value", `{"forward":{` + fwd + `},"other":{"a":1,"A":2}}`, false},
		{"repeated next", `{"forward":{` + fwd + `,"next":{"a":1},"next":{}}}`, true},
		{"repeated next, other case", `{"forward":{` + fwd + `,"next":{"a":1},"NEXT":{}}}`, true},
		{"repeated next, escaped", `{"forward":{` + fwd + `,"next":{"a":1},"next":{}}}`, true},
		{"repeated receiver", `{"forward":{` + fwd + `,"receiver":"r"}}`, true},
		{"repeated retries, Unicode fold", `{"forward":{` + fwd + `,"retries":1,"retrie` + "ſ" + `":2}}`, true},
		{"repeated forward, other case", `{"forward":{` + fwd + `},"Forward":{"next":{}}}`, true},
		{"repeated other top-level key", `{"forward":{` + fwd + `},"a":1,"a":2}`, true},
		{"repeated top-level key, other case", `{"forward":{` + fwd + `},"a":1,"A":2}`, true},
		{"repeated key inside next", `{"forward":{` + fwd + `,"next":{"a":1,"b":2,"a":3}}}`, true},
		{"repeated key deep inside next", `{"forward":{` + fwd + `,"next":{"x":[{"a":1,"a":2}]}}}`, true},
		{"repeated key inside other value", `{"forward":{` + fwd + `},"other":{"a":1,"a":2}}`, true},
		{"same key in sibling objects", `{"forward":{` + fwd + `,"next":{"x":{"a":1},"y":{"a":1}}}}`, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, rejectPFMForwardMemo(transferPacketData(t, tc.memo)))
		})
	}
}

// TestPFMMemoWithinLimitsMalformed checks that the limit check returns a
// result and does not panic on malformed JSON.
func TestPFMMemoWithinLimitsMalformed(t *testing.T) {
	for _, bz := range []string{``, ` `, `{`, `}`, `]`, `[}`, `{]`, `{"a"`, `{"a":}`, `{"a":1,}`, `"`, `{"forward":{"next":"`, `{"forward":{"NEXT":"\"`, "\xff", `{"\xff":1,"\xfe":2}`, `1 2`, `{}}`} {
		require.NotPanics(t, func() {
			pfmMemoWithinLimits([]byte(bz), true)
			pfmMemoWithinLimits([]byte(bz), false)
			pfmStringNextWithinLimits(bz)
			_ = validateForwardMemo(bz)
		}, bz)
	}
	// The entry points that expect valid JSON return false on malformed
	// input instead of panicking.
	for _, bz := range []string{``, ` `, `}`, `]`, `{}}`, `[]]`, `{"a":1}}`} {
		require.NotPanics(t, func() {
			require.False(t, pfmForwardMemoWithinLimits(bz), bz)
			require.False(t, pfmNextMemoWithinLimits(bz), bz)
			require.False(t, pfmWalkMemo(bz, true), bz)
			require.False(t, pfmWalkMemo(bz, false), bz)
		}, bz)
	}
}

// TestPFMForwardMemoDepthLimitIs16 checks the depth limit at 16 levels and
// one past it, for a forward memo and a string next.
func TestPFMForwardMemoDepthLimitIs16(t *testing.T) {
	for depth, want := range map[int]bool{16: false, 17: true} {
		require.Equal(t, want, rejectPFMForwardMemo(transferPacketData(t, pfmForwardMemo(depth, 0))), "forward memo depth %d", depth)
		require.Equal(t, want, rejectPFMForwardMemo(transferPacketData(t, pfmForwardMemoWithNext(jsonString(nestedJSON(depth)), 0))), "string next depth %d", depth)
	}
}

// TestPFMNextTokenMatchesDecoder checks that pfmNextToken returns the same
// tokens as json.Decoder.Token with UseNumber, which the limit check on main
// uses.
func TestPFMNextTokenMatchesDecoder(t *testing.T) {
	for _, s := range []string{
		`{}`, `[]`, `null`, `true`, `-1.5e+10`, `"x"`,
		` { "a" : [ 1 , -0.5E-3 , true , false , null , "s" , { } , [ ] ] , "b" : { "c" : "d" } } `,
		`{"forward":{"receiver":"r","next":"{\"a\":\"\\u00e9\"}","Next":null}}`,
		`{"\u0061":"\ud83d\ude00","\ud800":"\/\b\f\n\r\t","é":"ſ"}`,
		"{\"\xff\":\"\xfe\",\"a\xffb\":1}",
		`{"\ud800\u0041":"\udc00\ud800\udc00\"\\","\u00e9\u0301":"a\u0000b"}`,
		"{\"\xed\xa0\x80\\n\":\"\xc3\\t\xa9\xf0\x9f\x98\"}",
		pfmRepeatedNextMemo(),
	} {
		require.True(t, json.Valid([]byte(s)), s)
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		var want []json.Token
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			want = append(want, tok)
		}
		var got []json.Token
		for i := 0; ; {
			tok, end, ok := pfmNextToken(s, i)
			if !ok {
				break
			}
			switch tok.kind {
			case '{', '}', '[', ']':
				got = append(got, json.Delim(tok.kind))
			case '"':
				got = append(got, tok.text)
			case 'n':
				got = append(got, nil)
			case 'v':
				switch tok.text {
				case "true":
					got = append(got, true)
				case "false":
					got = append(got, false)
				default:
					got = append(got, json.Number(tok.text))
				}
			}
			i = end
		}
		require.Equal(t, want, got, s)
	}
}
