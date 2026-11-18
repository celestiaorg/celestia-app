package app

import (
	"testing"

	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	zkismtypes "github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/gogoproto/proto"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

const (
	bankSendTypeURL  = "/cosmos.bank.v1beta1.MsgSend"
	zkSubmitTypeURL  = "/celestia.zkism.v1.MsgSubmitMessages"
	ismProcessTypURL = "/hyperlane.core.v1.MsgProcessMessage"
)

// minimalVarint encodes v as a minimal proto varint.
func minimalVarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

// paddedVarint encodes v, which must fit in 7 bits, as a non-minimal varint of
// n bytes: continuation bytes carrying zero bits after the first.
func paddedVarint(v byte, n int) []byte {
	out := make([]byte, n)
	out[0] = v | 0x80
	for i := 1; i < n-1; i++ {
		out[i] = 0x80
	}
	return out
}

// protoField encodes a length delimited field with the given raw tag bytes.
func protoField(tag, value []byte) []byte {
	out := append([]byte{}, tag...)
	out = append(out, minimalVarint(uint64(len(value)))...)
	return append(out, value...)
}

// protoAny encodes an Any with one type_url field per typeURL, in order, and
// an empty value.
func protoAny(typeURLs ...string) []byte {
	out := make([]byte, 0, 2+len(typeURLs)*64)
	for _, typeURL := range typeURLs {
		out = append(out, protoField([]byte{0x0A}, []byte(typeURL))...)
	}
	return append(out, 0x12, 0x00)
}

// TestCountProtoAnyLastTypeURLWins checks that an Any with several type_url
// fields counts under the last one, as the host decodes it.
func TestCountProtoAnyLastTypeURLWins(t *testing.T) {
	cdc := encoding.MakeConfig(ModuleEncodingRegisters...).Codec
	testCases := []struct {
		name     string
		typeURLs []string
		wantZK   int
		wantISM  int
	}{
		{name: "benign then ZK", typeURLs: []string{bankSendTypeURL, zkSubmitTypeURL}, wantZK: 1},
		{name: "ZK then benign", typeURLs: []string{zkSubmitTypeURL, bankSendTypeURL}, wantZK: 0},
		{name: "benign then ISM", typeURLs: []string{bankSendTypeURL, ismProcessTypURL}, wantISM: appconsts.MaxISMSignatures + 1},
		{name: "ISM then benign", typeURLs: []string{ismProcessTypURL, bankSendTypeURL}, wantISM: 0},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload := protoField([]byte{0x0A}, protoAny(tc.typeURLs...))
			dispatched, err := icatypes.DeserializeCosmosTx(cdc, payload, icatypes.EncodingProtobuf)
			require.NoError(t, err)
			require.Len(t, dispatched, 1)
			require.Equal(t, tc.typeURLs[len(tc.typeURLs)-1], sdk.MsgTypeURL(dispatched[0]))

			requireNoUndercount(t, cdc, payload)
			require.Equal(t, tc.wantZK, countProtoWeighted(payload, zkProofTypeURLWeight, appconsts.MaxZKProofMessages+1))
			require.Equal(t, tc.wantISM, countProtoWeighted(payload, ismTypeURLWeight, appconsts.MaxISMSignatures+1))
		})
	}
}

// TestCountProtoTenByteVarints checks that the walk accepts the non-minimal
// ten-byte varints the host's generated decoder accepts, and rejects eleven.
func TestCountProtoTenByteVarints(t *testing.T) {
	cdc := encoding.MakeConfig(ModuleEncodingRegisters...).Codec
	sendAny := protoAny(bankSendTypeURL)
	zkAny := append(paddedVarint(0x0A, 10), minimalVarint(uint64(len(zkSubmitTypeURL)))...)
	zkAny = append(append(zkAny, zkSubmitTypeURL...), 0x12, 0x00)

	testCases := []struct {
		name    string
		payload []byte
		wantMsg int
		wantZK  int
	}{
		{
			name:    "ten-byte tag",
			payload: protoField(paddedVarint(0x0A, 10), sendAny),
			wantMsg: 1,
		},
		{
			name:    "ten-byte length",
			payload: append(append([]byte{0x0A}, paddedVarint(byte(len(sendAny)), 10)...), sendAny...),
			wantMsg: 1,
		},
		{
			name:    "ten-byte type_url tag in the Any",
			payload: protoField([]byte{0x0A}, zkAny),
			wantMsg: 1,
			wantZK:  1,
		},
		{
			name:    "eleven-byte tag",
			payload: protoField(paddedVarint(0x0A, 11), sendAny),
			wantMsg: -1,
			wantZK:  -1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dispatched, err := icatypes.DeserializeCosmosTx(cdc, tc.payload, icatypes.EncodingProtobuf)
			if tc.wantMsg < 0 {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Len(t, dispatched, tc.wantMsg)
			}
			requireNoUndercount(t, cdc, tc.payload)
			require.Equal(t, tc.wantMsg, countProtoMsgs(tc.payload))
			require.Equal(t, tc.wantZK, countProtoWeighted(tc.payload, zkProofTypeURLWeight, appconsts.MaxZKProofMessages+1))
		})
	}
}

// TestWalkProtoMsgRejectsWhatTheHostRejects checks that malformed fields the
// host's generated decoder rejects make the proto counter reject the payload,
// so the packet count falls back to the other encoding or fails closed.
func TestWalkProtoMsgRejectsWhatTheHostRejects(t *testing.T) {
	cdc := encoding.MakeConfig(ModuleEncodingRegisters...).Codec
	sendEntry := protoField([]byte{0x0A}, protoAny(bankSendTypeURL))
	payloads := map[string][]byte{
		// Field number zero, length delimited.
		"field number zero": append([]byte{0x02, 0x00}, sendEntry...),
		// An end group for field 2 with no start, then a start group for
		// field 3 that would bring a lenient depth count back to zero.
		"lone end group": append([]byte{0x14, 0x1B}, sendEntry...),
		// An end group tag on the messages field itself.
		"end group on messages field": append([]byte{0x0C}, sendEntry...),
		// The messages field as a varint instead of length delimited.
		"messages field as varint": append([]byte{0x08, 0x00}, sendEntry...),
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			_, err := icatypes.DeserializeCosmosTx(cdc, payload, icatypes.EncodingProtobuf)
			require.Error(t, err)
			require.Equal(t, -1, countProtoMsgs(payload))
			require.Equal(t, -1, countProtoWeighted(payload, zkProofTypeURLWeight, appconsts.MaxZKProofMessages+1))
			packet := icaPacketWithPayload(payload).(*channeltypes.MsgRecvPacket).Packet
			require.Equal(t, appconsts.MaxSDKMessages, countICAPacketMsgs(packet))
			require.Equal(t, appconsts.MaxZKProofMessages+1, countICAPacketZKProofMsgs(packet))
		})
	}
}

// TestCountJSONWeightedRejectsNonStringType checks that a non-string "@type",
// which the host rejects, does not count as an entry of no type.
func TestCountJSONWeightedRejectsNonStringType(t *testing.T) {
	cdc := encoding.MakeConfig(ModuleEncodingRegisters...).Codec
	for _, payload := range []string{
		`{"messages":[{"@type":1}]}`,
		`{"messages":[{"@type":["/celestia.zkism.v1.MsgSubmitMessages"]}]}`,
	} {
		_, err := icatypes.DeserializeCosmosTx(cdc, []byte(payload), icatypes.EncodingProto3JSON)
		require.Error(t, err, payload)
		require.Equal(t, -1, countJSONWeighted([]byte(payload), zkProofTypeURLWeight, appconsts.MaxZKProofMessages+1), payload)
		require.Equal(t, -1, countJSONWeighted([]byte(payload), ismTypeURLWeight, appconsts.MaxISMSignatures+1), payload)
	}
}

// TestICAPacketOnOtherPortWeighsAsPlainMessage checks that a packet bound for a
// port other than the ICA host weighs one, whatever its data says.
func TestICAPacketOnOtherPortWeighsAsPlainMessage(t *testing.T) {
	cdc := encoding.MakeConfig(ModuleEncodingRegisters...).Codec
	inner := []proto.Message{&zkismtypes.MsgSubmitMessages{}, &zkismtypes.MsgSubmitMessages{}, &hyperlanetypes.MsgProcessMessage{}}
	payload, err := icatypes.SerializeCosmosTx(cdc, inner, icatypes.EncodingProtobuf)
	require.NoError(t, err)

	host := icaPacketWithPayload(payload)
	require.Equal(t, 1+len(inner), countExecutableMsgs([]sdk.Msg{host}))
	require.Equal(t, 2, countZKProofMsgs([]sdk.Msg{host}))
	require.Equal(t, appconsts.MaxISMSignatures+1, countISMSignatures([]sdk.Msg{host}))

	data := icatypes.InterchainAccountPacketData{Type: icatypes.EXECUTE_TX, Data: payload}
	packet := channeltypes.NewPacket(data.GetBytes(), 1, "icacontroller-test", "channel-0", "transfer", "channel-1", clienttypes.ZeroHeight(), 0)
	other := channeltypes.NewMsgRecvPacket(packet, nil, clienttypes.ZeroHeight(), "signer")
	require.Equal(t, 1, countExecutableMsgs([]sdk.Msg{other}))
	require.Zero(t, countZKProofMsgs([]sdk.Msg{other}))
	require.Zero(t, countISMSignatures([]sdk.Msg{other}))
}

// TestBlockMsgCountsAccumulate checks that each count ProcessProposal caps adds
// up across the txs of a block rather than resetting per tx.
func TestBlockMsgCountsAccumulate(t *testing.T) {
	repeat := func(n int, msg func() sdk.Msg) []sdk.Msg {
		msgs := make([]sdk.Msg, n)
		for i := range msgs {
			msgs[i] = msg()
		}
		return msgs
	}
	send := func() sdk.Msg { return &banktypes.MsgSend{} }
	proof := func() sdk.Msg { return &zkismtypes.MsgSubmitMessages{} }
	process := func(sigs int) []sdk.Msg {
		return []sdk.Msg{&hyperlanetypes.MsgProcessMessage{Metadata: ismMetadataHex(sigs)}}
	}

	t.Run("SDK messages", func(t *testing.T) {
		var counts blockMsgCounts
		require.Empty(t, counts.add(repeat(appconsts.MaxSDKMessages/2, send)))
		require.Empty(t, counts.add(repeat(appconsts.MaxSDKMessages/2, send)))
		require.Contains(t, counts.add(repeat(1, send)), "max SDK message count")
	})
	t.Run("ZK proofs", func(t *testing.T) {
		var counts blockMsgCounts
		require.Empty(t, counts.add(repeat(appconsts.MaxZKProofMessages/2, proof)))
		require.Empty(t, counts.add(repeat(appconsts.MaxZKProofMessages/2, proof)))
		require.Contains(t, counts.add(repeat(1, proof)), "max ZK proof message count")
	})
	t.Run("ISM signatures", func(t *testing.T) {
		var counts blockMsgCounts
		require.Empty(t, counts.add(process(appconsts.MaxISMSignatures/2)))
		require.Empty(t, counts.add(process(appconsts.MaxISMSignatures/2)))
		require.Contains(t, counts.add(process(1)), "max ISM signature count")
	})
}

// TestGogoSkipRejectsLoneEndGroup checks gogoSkip on its own, like the
// generated skip function: an end group with no start group is an error. The
// walk also rejects an end group tag before it skips, so only a direct call
// reaches this check.
func TestGogoSkipRejectsLoneEndGroup(t *testing.T) {
	_, ok := gogoSkip([]byte{0x14, 0x1B, 0x1C})
	require.False(t, ok)
	n, ok := gogoSkip([]byte{0x13, 0x08, 0x01, 0x14, 0x0A})
	require.True(t, ok)
	require.Equal(t, 4, n)
}
