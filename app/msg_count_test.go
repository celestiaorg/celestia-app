package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/gogoproto/proto"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// fakeChannelKeeper reports the encoding an ICA channel negotiated. An empty
// encoding means the channel does not exist.
type fakeChannelKeeper struct{ encoding string }

func (f fakeChannelKeeper) GetAppVersion(sdk.Context, string, string) (string, bool) {
	if f.encoding == "" {
		return "", false
	}
	return string(icatypes.ModuleCdc.MustMarshalJSON(&icatypes.Metadata{Encoding: f.encoding})), true
}

func TestCountExecutableMsgs(t *testing.T) {
	cdc := encoding.MakeConfig(ModuleEncodingRegisters...).Codec
	send := func() sdk.Msg { return &banktypes.MsgSend{} }

	msgExecWith := func(msgs ...sdk.Msg) sdk.Msg {
		exec := authz.NewMsgExec(sdk.AccAddress{}, msgs)
		return &exec
	}
	sends := func(n int) []sdk.Msg {
		msgs := make([]sdk.Msg, n)
		for i := range msgs {
			msgs[i] = send()
		}
		return msgs
	}

	// icaPayload serializes n bank sends the way an ICA controller would.
	icaPayload := func(n int, enc string) []byte {
		msgs := make([]proto.Message, n)
		for i := range msgs {
			msgs[i] = &banktypes.MsgSend{}
		}
		bz, err := icatypes.SerializeCosmosTx(cdc, msgs, enc)
		require.NoError(t, err)
		return bz
	}
	// recvPacket wraps payload in a MsgRecvPacket addressed to destPort.
	recvPacket := func(destPort string, data []byte) sdk.Msg {
		packet := channeltypes.NewPacket(data, 1, "icacontroller-test", "channel-0", destPort, "channel-1", clienttypes.ZeroHeight(), 0)
		return channeltypes.NewMsgRecvPacket(packet, nil, clienttypes.ZeroHeight(), "signer")
	}
	// icaRecvPacket builds a MsgRecvPacket the ICA host would execute n messages from.
	icaRecvPacket := func(n int, enc string) sdk.Msg {
		data := icatypes.InterchainAccountPacketData{Type: icatypes.EXECUTE_TX, Data: icaPayload(n, enc)}
		return recvPacket(icatypes.HostPortID, data.GetBytes())
	}

	moduleQuerySafeWith := func(n int) sdk.Msg {
		requests := make([]*icahosttypes.QueryRequest, n)
		for i := range requests {
			requests[i] = &icahosttypes.QueryRequest{Path: "/cosmos.bank.v1beta1.Query/TotalSupply"}
		}
		return icahosttypes.NewMsgModuleQuerySafe("", requests)
	}

	testCases := []struct {
		name string
		msgs []sdk.Msg
		// encoding the ICA channel negotiated; empty means no such channel.
		encoding string
		expected int
	}{
		{
			name:     "plain messages count as one each",
			msgs:     []sdk.Msg{send(), send(), send()},
			expected: 3,
		},
		{
			name:     "MsgExec counts its inner messages",
			msgs:     []sdk.Msg{msgExecWith(sends(99)...)},
			expected: 99,
		},
		{
			name:     "mix of plain and MsgExec",
			msgs:     []sdk.Msg{msgExecWith(sends(2)...), send(), msgExecWith(sends(3)...)},
			expected: 6,
		},
		{
			name:     "empty MsgExec counts as zero",
			msgs:     []sdk.Msg{msgExecWith()},
			expected: 0,
		},
		{
			name:     "MsgModuleQuerySafe counts its queries",
			msgs:     []sdk.Msg{moduleQuerySafeWith(5)},
			expected: 5,
		},
		{
			name:     "MsgModuleQuerySafe inside MsgExec counts its queries",
			msgs:     []sdk.Msg{msgExecWith(moduleQuerySafeWith(5))},
			expected: 5,
		},
		{
			name:     "MsgModuleQuerySafe with no queries still counts as one",
			msgs:     []sdk.Msg{moduleQuerySafeWith(0)},
			expected: 1,
		},
		{
			name:     "mix of MsgModuleQuerySafe and plain messages",
			msgs:     []sdk.Msg{moduleQuerySafeWith(3), send(), msgExecWith(moduleQuerySafeWith(2), send())},
			expected: 7,
		},
		{
			name:     "proto3 ICA packet counts its payload messages plus the packet",
			msgs:     []sdk.Msg{icaRecvPacket(50, icatypes.EncodingProtobuf)},
			encoding: icatypes.EncodingProtobuf,
			expected: 51,
		},
		{
			name:     "proto3json ICA packet counts its payload messages plus the packet",
			msgs:     []sdk.Msg{icaRecvPacket(50, icatypes.EncodingProto3JSON)},
			encoding: icatypes.EncodingProto3JSON,
			expected: 51,
		},
		{
			name:     "empty ICA payload counts only the packet",
			msgs:     []sdk.Msg{icaRecvPacket(0, icatypes.EncodingProtobuf)},
			encoding: icatypes.EncodingProtobuf,
			expected: 1,
		},
		{
			name:     "packet to another port counts as one",
			encoding: icatypes.EncodingProtobuf,
			msgs:     []sdk.Msg{recvPacket("transfer", icaPayload(50, icatypes.EncodingProtobuf))},
			expected: 1,
		},
		{
			// Malformed packet data fails the same decode the host uses, so the
			// host dispatches nothing and this is safe to count as just the packet.
			name:     "malformed ICA packet data counts as one",
			encoding: icatypes.EncodingProtobuf,
			msgs:     []sdk.Msg{recvPacket(icatypes.HostPortID, []byte("not json"))},
			expected: 1,
		},
		{
			name:     "non executable ICA packet type counts as one",
			encoding: icatypes.EncodingProtobuf,
			msgs: []sdk.Msg{func() sdk.Msg {
				data := icatypes.InterchainAccountPacketData{Type: icatypes.UNSPECIFIED, Data: icaPayload(50, icatypes.EncodingProtobuf)}
				return recvPacket(icatypes.HostPortID, data.GetBytes())
			}()},
			expected: 1,
		},
		{
			name:     "ICA packet inside a MsgExec counts its payload messages",
			msgs:     []sdk.Msg{msgExecWith(icaRecvPacket(50, icatypes.EncodingProtobuf))},
			encoding: icatypes.EncodingProtobuf,
			expected: 51,
		},
		{
			// Without the channel the encoding is unknown, so the payload cannot
			// be counted and must not be counted as free.
			name:     "ICA packet on an unknown channel counts as over the limit",
			msgs:     []sdk.Msg{icaRecvPacket(50, icatypes.EncodingProtobuf)},
			expected: 1 + appconsts.MaxSDKMessages,
		},
		{
			// The payload is proto3json but the channel negotiated proto3, so the
			// proto3 counter cannot read it and it fails closed.
			name:     "ICA payload not matching the channel encoding counts as over the limit",
			msgs:     []sdk.Msg{icaRecvPacket(50, icatypes.EncodingProto3JSON)},
			encoding: icatypes.EncodingProtobuf,
			expected: 1 + appconsts.MaxSDKMessages,
		},
		{
			name: "undecodable message inside a MsgExec counts as one",
			msgs: []sdk.Msg{&authz.MsgExec{Msgs: []*codectypes.Any{{
				TypeUrl: sdk.MsgTypeURL(&channeltypes.MsgRecvPacket{}),
				Value:   []byte("not a MsgRecvPacket"),
			}}}},
			expected: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ck := fakeChannelKeeper{encoding: tc.encoding}
			require.Equal(t, tc.expected, countExecutableMsgs(sdk.Context{}, ck, tc.msgs))
		})
	}
}

// TestCountICAPacketMsgsNeverUndercounts checks the counter against the decoder
// the ICA host actually uses. For any payload the host can decode, the count
// must be at least the number of messages the host would dispatch, otherwise a
// packet could execute more messages than the block limit accounts for.
func TestCountICAPacketMsgsNeverUndercounts(t *testing.T) {
	cdc := encoding.MakeConfig(ModuleEncodingRegisters...).Codec
	send := &banktypes.MsgSend{}
	msgs := []proto.Message{send, send, send}

	protoPayload, err := icatypes.SerializeCosmosTx(cdc, msgs, icatypes.EncodingProtobuf)
	require.NoError(t, err)
	jsonPayload, err := icatypes.SerializeCosmosTx(cdc, msgs, icatypes.EncodingProto3JSON)
	require.NoError(t, err)

	// A proto3json payload laid out so its bytes are also a walkable proto wire
	// stream: the leading "\n " is JSON whitespace and a proto field 1 tag of
	// length 32, and the "*t" in each element is a field 5 tag whose length
	// strides exactly one element. Counting it under proto3 yields 1 while the
	// host, on a proto3json channel, dispatches every message.
	element := `{"fromAddress":"AAA*tAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","@type":"/cosmos.bank.v1beta1.MsgSend"}`
	crafted := []byte("\n {\"messages\":[")
	for i := range 700 {
		if i > 0 {
			crafted = append(crafted, ',')
		}
		crafted = append(crafted, element...)
	}
	crafted = append(crafted, []byte("]}                  ")...)

	payloads := map[string][]byte{
		"proto3":                   protoPayload,
		"proto3json":               jsonPayload,
		"trailing byte":            append(append([]byte{}, jsonPayload...), 'X'),
		"leading whitespace":       append([]byte{'\n', ' '}, jsonPayload...),
		"duplicate key, number":    append([]byte(`{"messages":0,`), jsonPayload[1:]...),
		"unknown sibling field":    append([]byte(`{"bogus":1,`), jsonPayload[1:]...),
		"null messages":            []byte(`{"messages":null}`),
		"empty":                    {},
		"garbage":                  []byte("not a payload at all"),
		"proto with group tag":     {0x0B, 0x0A, 0x00, 0x0C},
		"proto truncated field":    {0x0A, 0x7F},
		"json that walks as proto": crafted,
	}

	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			requireNoUndercount(t, cdc, payload)
		})
	}
}

// FuzzCountICAPacketMsgs is the same property as
// TestCountICAPacketMsgsNeverUndercounts over generated payloads. The counter
// does not use the host's decoders, so the two can disagree in ways a fixed
// table does not anticipate; only an undercount is a bug.
func FuzzCountICAPacketMsgs(f *testing.F) {
	cdc := encoding.MakeConfig(ModuleEncodingRegisters...).Codec
	f.Add([]byte{0x0A, 0x00})
	f.Add([]byte{0x0B, 0x0A, 0x00, 0x0C})
	f.Add([]byte(`{"messages":[]}`))
	f.Add([]byte(`{"messages":[{"@type":"/cosmos.bank.v1beta1.MsgSend","amount":[]}]}`))
	f.Add([]byte("\n {\"messages\":[{\"fromAddress\":\"AAA*tAA\",\"@type\":\"/cosmos.bank.v1beta1.MsgSend\"}]}  "))

	f.Fuzz(func(t *testing.T, payload []byte) {
		requireNoUndercount(t, cdc, payload)
	})
}

// requireNoUndercount fails if either counter reports fewer messages than the
// ICA host would dispatch from the same payload under that encoding. Counting
// more is safe; counting fewer lets a packet execute messages the block limit
// never accounted for.
func requireNoUndercount(t *testing.T, cdc codec.Codec, payload []byte) {
	t.Helper()
	counters := map[string]func([]byte) int{
		icatypes.EncodingProtobuf:   countProtoMsgs,
		icatypes.EncodingProto3JSON: countJSONMsgs,
	}
	for enc, count := range counters {
		dispatched, err := icatypes.DeserializeCosmosTx(cdc, payload, enc)
		if err != nil {
			continue // the host dispatches nothing under this encoding
		}
		counted := count(payload)
		if counted < 0 {
			continue // countICAPacketMsgs fails closed on this
		}
		require.GreaterOrEqual(t, counted, len(dispatched),
			"counted %d but the host would dispatch %d under %s", counted, len(dispatched), enc)
	}
}
