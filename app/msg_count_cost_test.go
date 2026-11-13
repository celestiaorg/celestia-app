package app

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	zkismtypes "github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/gogoproto/proto"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	"github.com/stretchr/testify/require"
)

// TestBlockMsgCountsChecksSDKMessagesFirst checks that ProcessProposal runs the
// executable message cap before the weighted walks, like CheckTx and
// PrepareProposal. The executable count stops at its cap; the weighted walks
// read every entry of an ICA payload.
func TestBlockMsgCountsChecksSDKMessagesFirst(t *testing.T) {
	sdkReason := "block exceeds max SDK message count of " + strconv.Itoa(appconsts.MaxSDKMessages)

	t.Run("tx over both the SDK message and ZK proof caps", func(t *testing.T) {
		msgs := make([]sdk.Msg, appconsts.MaxSDKMessages+1)
		for i := range msgs {
			msgs[i] = &zkismtypes.MsgSubmitMessages{}
		}
		var counts blockMsgCounts
		require.Equal(t, sdkReason, counts.add(msgs))
	})

	t.Run("ICA packet with many empty entries", func(t *testing.T) {
		payload := bytes.Repeat([]byte{0x0A, 0x00}, 200_000)
		var counts blockMsgCounts
		require.Equal(t, sdkReason, counts.add([]sdk.Msg{icaPacketWithPayload(payload)}))
	})
}

// TestTypeURLHelpersDoNotAllocate checks that the type URL checks run once per
// ICA payload entry compare against precomputed strings.
func TestTypeURLHelpersDoNotAllocate(t *testing.T) {
	typeURL := "/cosmos.bank.v1beta1.MsgSend"
	allocs := testing.AllocsPerRun(100, func() {
		_ = isFanOutTypeURL(typeURL)
		_ = isZKProofTypeURL(typeURL)
		_ = ismTypeURLWeight(typeURL)
	})
	require.Zero(t, allocs)
}

// jsonCosmosTxMsgsMap is the map based reading jsonCosmosTxMsgs replaced. It
// matches the host on every payload with no unknown top-level key.
func jsonCosmosTxMsgsMap(bz []byte) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(bz)).Decode(&fields); err != nil {
		return nil, false
	}
	return fields["messages"], true
}

func TestJSONCosmosTxMsgs(t *testing.T) {
	testCases := []struct {
		name    string
		payload string
		// unknownKey marks a payload with a top-level key other than
		// "messages", which the host rejects and the map reading accepted.
		unknownKey bool
	}{
		{name: "null", payload: `null`},
		{name: "empty object", payload: `{}`},
		{name: "empty input", payload: ``},
		{name: "whitespace only", payload: "  \n"},
		{name: "messages", payload: `{"messages":[{"@type":"/a"},{}]}`},
		{name: "messages null", payload: `{"messages":null}`},
		{name: "messages empty", payload: `{"messages":[]}`},
		{name: "messages number", payload: `{"messages":7}`},
		{name: "duplicate, list last", payload: `{"messages":0,"messages":[{},{}]}`},
		{name: "duplicate, list first", payload: `{"messages":[{},{}],"messages":0}`},
		{name: "duplicate, null last", payload: `{"messages":[{}],"messages":null}`},
		{name: "trailing bytes", payload: `{"messages":[{}]} trailing`},
		{name: "second value", payload: `{"messages":[{}]}{"messages":[{},{}]}`},
		{name: "leading whitespace", payload: "\n {\"messages\":[{}]}"},
		{name: "escaped key", payload: `{"messages":[{},{}]}`},
		{name: "trailing comma", payload: `{"messages":[],}`},
		{name: "unterminated object", payload: `{"messages":[]`},
		{name: "missing value", payload: `{"messages":}`},
		{name: "bad value", payload: `{"messages":[}`},
		{name: "array top level", payload: `[]`},
		{name: "string top level", payload: `"messages"`},
		{name: "number top level", payload: `1`},
		{name: "bool top level", payload: `true`},
		{name: "garbage", payload: `not json`},
		{name: "case-folded key", payload: `{"Messages":[{}]}`, unknownKey: true},
		{name: "case-folded sibling", payload: `{"messages":[{}],"Messages":[]}`, unknownKey: true},
		{name: "unknown key first", payload: `{"x":1,"messages":[{}]}`, unknownKey: true},
		{name: "unknown key last", payload: `{"messages":[{}],"x":1}`, unknownKey: true},
		{name: "unknown key, bad syntax after", payload: `{"x":1,`, unknownKey: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			raw, ok := jsonCosmosTxMsgs([]byte(tc.payload))
			if tc.unknownKey {
				require.False(t, ok)
				return
			}
			wantRaw, wantOK := jsonCosmosTxMsgsMap([]byte(tc.payload))
			require.Equal(t, wantOK, ok)
			require.Equal(t, wantRaw == nil, raw == nil)
			require.Equal(t, string(wantRaw), string(raw))
		})
	}
}

// TestJSONCosmosTxMsgsManyKeys checks that a payload with many distinct
// top-level keys is rejected without building a map of them.
func TestJSONCosmosTxMsgsManyKeys(t *testing.T) {
	var b strings.Builder
	b.WriteByte('{')
	for i := range 100_000 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"` + strconv.Itoa(i) + `":0`)
	}
	b.WriteByte('}')
	payload := []byte(b.String())

	allocs := testing.AllocsPerRun(10, func() {
		_, ok := jsonCosmosTxMsgs(payload)
		require.False(t, ok)
	})
	require.Less(t, allocs, 50.0)
}

// TestCountICAPacketMsgsUnknownJSONField checks that a proto3json payload with
// an unknown top-level key, which the host rejects, still fails closed rather
// than counting fewer messages than it carries.
func TestCountICAPacketMsgsUnknownJSONField(t *testing.T) {
	cdc := encoding.MakeConfig(ModuleEncodingRegisters...).Codec
	inner := make([]proto.Message, appconsts.MaxSDKMessages)
	for i := range inner {
		inner[i] = &banktypes.MsgSend{}
	}
	jsonPayload, err := icatypes.SerializeCosmosTx(cdc, inner, icatypes.EncodingProto3JSON)
	require.NoError(t, err)
	body := jsonPayload[1 : len(jsonPayload)-1]

	payloads := map[string][]byte{
		"case-folded sibling": []byte(`{` + string(body) + `,"Messages":[]}`),
		"unknown key first":   []byte(`{"x":1,` + string(body) + `}`),
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			for _, enc := range []string{icatypes.EncodingProtobuf, icatypes.EncodingProto3JSON} {
				_, err := icatypes.DeserializeCosmosTx(cdc, payload, enc)
				require.Error(t, err, "host decodes the payload under %s", enc)
			}
			requireNoUndercount(t, cdc, payload)
			packet := icaPacketWithPayload(payload)
			require.Greater(t, countExecutableMsgs([]sdk.Msg{packet}), appconsts.MaxSDKMessages)
		})
	}
}
