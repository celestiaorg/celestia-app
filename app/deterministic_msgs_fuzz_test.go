package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	pfmtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// The fuzz targets in this file check the packet-forward memo filter in deterministic_msgs.go against
// independent oracles built on encoding/json and the packet-forward-middleware types, not against the
// filter's own scanner.

// FuzzValidateTransferPacketData checks validateTransferPacketData and validateDeterministicMsgs on arbitrary
// packet data.
func FuzzValidateTransferPacketData(f *testing.F) {
	ics20 := func(extra string) string {
		return `{"amount":"1","denom":"utia","receiver":"r","sender":"s"` + extra + `}`
	}
	for _, memo := range fuzzForwardMemoSeeds() {
		data, err := json.Marshal(transfertypes.FungibleTokenPacketData{Denom: "utia", Amount: "1", Sender: "s", Receiver: "r", Memo: memo})
		require.NoError(f, err)
		f.Add(data)
	}
	for _, data := range []string{
		``, `null`, `[1]`, `{}`, `"x"`, `{`, `{"memo":`,
		ics20(``),
		ics20(`,"memo":""`),
		ics20(`,"\u006demo":"x"`),
		ics20(`,"forward":5`),
		ics20(`,"Memo":"x"`),
		ics20(`,"memo":"","memo":"x"`),
		ics20(`,"memo":"{\"forward\":{\"receiver\":\"x\",\"port\":\"transfer\",\"channel\":\"channel-7\"}}"`),
		ics20(`,"memo":"{\"forward\":{\"receiver\":\"x\",\"port\":\"transfer\",\"channel\":\"channel-7\"}}","memo":""`),
		ics20(`,"memo":"{\"forward\":{\"receiver\":\"x\",\"port\":\"transfer\",\"channel\":\"channel-7\",\"timeout\":1e19}}"`),
		ics20(`,"memo":"{\"forward\":{\"receiver\":\"\\ud800\",\"port\":\"transfer\",\"channel\":\"channel-7\"}}"`),
		ics20(`,"memo":"{\"forward\":{\"receiver\":\"x\",\"port\":\"transfer\",\"channel\":\"channel-7\"}}"} trailing`),
		`{"amount":1,"denom":"utia","memo":"{\"forward\":{}}"}`,
		"{\"memo\":\"\xff\",\"amount\":\"1\"}",
		` { "memo" : "{\"forward\":null}" , "amount" : "1" } `,
	} {
		f.Add([]byte(data))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		err := validateTransferPacketData(data)
		// Determinism: the same input gives the same result.
		requireSameError(t, err, validateTransferPacketData(data))
		if len(data) > maxTransferPacketDataSize {
			require.Error(t, err)
		}
		requireDeterministicMsgsAgree(t, data, err)

		var ftpd transfertypes.FungibleTokenPacketData
		if transfertypes.ModuleCdc.UnmarshalJSON(data, &ftpd) != nil {
			// ICS-20 and the middleware ignore packet data that does not decode.
			return
		}
		// The filter accepts decodable packet data exactly when its keys are unambiguous and its memo is
		// accepted.
		keysErr := ics20KeysOracle(data)
		memoErr := validateForwardMemo(ftpd.Memo)
		require.Equal(t, keysErr == nil && memoErr == nil, err == nil, "filter: %v, keys oracle: %v, memo: %v", err, keysErr, memoErr)
		if err != nil {
			return
		}
		// Decoders that pick a different value for a repeated or case-variant key would disagree here.
		var std transfertypes.FungibleTokenPacketData
		if json.Unmarshal(data, &std) == nil {
			require.Equal(t, ftpd.Memo, std.Memo)
		}
		requireForwardMemoUnambiguous(t, ftpd.Memo)
	})
}

// FuzzValidateForwardMemo checks validateForwardMemo on arbitrary memos.
func FuzzValidateForwardMemo(f *testing.F) {
	for _, memo := range fuzzForwardMemoSeeds() {
		f.Add(memo)
	}

	f.Fuzz(func(t *testing.T, memo string) {
		err := validateForwardMemo(memo)
		requireSameError(t, err, validateForwardMemo(memo))
		if utf8.ValidString(memo) {
			// The packet-level check gives the same result for a memo carried in well-formed packet data.
			data, mErr := json.Marshal(transfertypes.FungibleTokenPacketData{Denom: "utia", Amount: "1", Sender: "s", Receiver: "r", Memo: memo})
			require.NoError(t, mErr)
			require.Equal(t, err == nil, validateTransferPacketData(data) == nil, "memo %q: %v", memo, err)
			if len(memo) <= transfertypes.MaximumMemoLength {
				data = transfertypes.NewFungibleTokenPacketData("utia", "1", "s", "r", memo).GetBytes()
				require.Equal(t, err == nil, validateTransferPacketData(data) == nil, "memo %q: %v", memo, err)
			}
		}
		if err == nil {
			requireForwardMemoUnambiguous(t, memo)
		}
	})
}

// FuzzForwardMemoGenerated builds honest forward memos from the fuzz input and checks that the filter accepts
// them. It then applies one mutation that makes the memo ambiguous or invalid, and checks that the filter
// rejects the result.
func FuzzForwardMemoGenerated(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0})
	f.Add(bytes.Repeat([]byte{1}, 64))
	f.Add(bytes.Repeat([]byte{2, 7, 3}, 40))
	f.Add([]byte("multi-hop forward with string next and escapes"))
	for m := range numMemoMutations {
		f.Add([]byte{3, 1, 4, 1, 5, 9, 2, 6, 5, 3, 5, 8, 9, 7, 9, 3, 2, 3, 8, 4, byte(m)})
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		src := &fuzzSource{b: input}
		root := genForwardMemo(src, 2)
		memo := root.json(src)
		require.LessOrEqual(t, jsonDepth(t, memo), maxPFMForwardMemoDepth, memo)
		require.LessOrEqual(t, len(memo), transfertypes.MaximumMemoLength, memo)
		require.NoError(t, validateForwardMemo(memo), memo)
		requireForwardMemoUnambiguous(t, memo)
		data, err := json.Marshal(transfertypes.FungibleTokenPacketData{Denom: "utia", Amount: "1", Sender: "s", Receiver: "r", Memo: memo})
		require.NoError(t, err)
		require.NoError(t, validateTransferPacketData(data), memo)
		require.NoError(t, validateTransferPacketData(transfertypes.NewFungibleTokenPacketData("utia", "1", "s", "r", memo).GetBytes()), memo)
		requireDeterministicMsgsAgree(t, data, nil)

		mutation := src.intn(numMemoMutations + 1)
		if mutation == 0 {
			return
		}
		packetLevel := mutateForwardMemo(src, root, mutation-1)
		bad := root.json(src)
		require.Error(t, validateForwardMemo(bad), "mutation %d: %s", mutation-1, bad)
		if packetLevel {
			data, err := json.Marshal(transfertypes.FungibleTokenPacketData{Denom: "utia", Amount: "1", Sender: "s", Receiver: "r", Memo: bad})
			require.NoError(t, err)
			require.Error(t, validateTransferPacketData(data), "mutation %d: %s", mutation-1, bad)
			requireDeterministicMsgsAgree(t, data, validateTransferPacketData(data))
		}
	})
}

// requireSameError fails unless a and b are both nil or have the same message.
func requireSameError(t *testing.T, a, b error) {
	t.Helper()
	if a == nil || b == nil {
		require.Equal(t, a == nil, b == nil, "%v != %v", a, b)
		return
	}
	require.Equal(t, a.Error(), b.Error())
}

// requireDeterministicMsgsAgree checks that validateDeterministicMsgs applies validateTransferPacketData, whose
// result for data is dataErr, to a MsgRecvPacket on the transfer port, directly, in a MsgExec and in a
// MsgSubmitProposal, and ignores it on another port.
func requireDeterministicMsgsAgree(t *testing.T, data []byte, dataErr error) {
	t.Helper()
	addr := sdk.AccAddress("addr________________")
	recv := func(port string) sdk.Msg {
		packet := channeltypes.NewPacket(data, 7, port, "channel-0", port, "channel-1", channeltypes.Packet{}.TimeoutHeight, 1)
		return channeltypes.NewMsgRecvPacket(packet, []byte("proof"), channeltypes.Packet{}.TimeoutHeight, addr.String())
	}
	transfer := recv(transfertypes.PortID)
	exec := authz.NewMsgExec(addr, []sdk.Msg{transfer})
	proposal, err := govv1.NewMsgSubmitProposal([]sdk.Msg{transfer}, nil, addr.String(), "", "title", "summary", false)
	require.NoError(t, err)
	for _, msgs := range [][]sdk.Msg{{transfer}, {&exec}, {proposal}} {
		err := validateDeterministicMsgs(msgsTx{msgs: msgs})
		if dataErr == nil {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
			require.True(t, utf8.ValidString(err.Error()))
		}
	}
	require.NoError(t, validateDeterministicMsgs(msgsTx{msgs: []sdk.Msg{recv("icahost")}}))
}

// ics20KeysOracle returns an error if the first JSON value in data is an object whose keys are not distinct
// ICS-20 field names, compared after unescaping.
func ics20KeysOracle(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		// A value that is not an object, such as null, has no keys.
		return nil
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key := tok.(string)
		switch key {
		case "amount", "denom", "memo", "receiver", "sender":
		default:
			return fmt.Errorf("unknown key %q", key)
		}
		if seen[key] {
			return fmt.Errorf("repeated key %q", key)
		}
		seen[key] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return err
		}
	}
	return nil
}

// jsonNode is a JSON value decoded with its object keys in order and repeated keys kept.
type jsonNode struct {
	kind  byte // '{', '[', '"', 'n' for null, or 'v' for a number, true or false
	keys  []string
	vals  []*jsonNode
	str   string
	depth int
}

// decodeJSONNode decodes the JSON text s, which must be valid, into a jsonNode.
func decodeJSONNode(t *testing.T, s string) *jsonNode {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var read func() *jsonNode
	read = func() *jsonNode {
		tok, err := dec.Token()
		require.NoError(t, err)
		switch v := tok.(type) {
		case json.Delim:
			n := &jsonNode{kind: byte(v), depth: 1}
			for dec.More() {
				if v == '{' {
					key, err := dec.Token()
					require.NoError(t, err)
					n.keys = append(n.keys, key.(string))
				}
				child := read()
				n.vals = append(n.vals, child)
				n.depth = max(n.depth, 1+child.depth)
			}
			_, err := dec.Token()
			require.NoError(t, err)
			return n
		case string:
			return &jsonNode{kind: '"', str: v}
		case nil:
			return &jsonNode{kind: 'n'}
		default:
			return &jsonNode{kind: 'v'}
		}
	}
	return read()
}

// jsonDepth returns the nesting depth of the valid JSON text s.
func jsonDepth(t *testing.T, s string) int {
	t.Helper()
	return decodeJSONNode(t, s).depth
}

// walk calls fn on n and every value inside it.
func (n *jsonNode) walk(fn func(*jsonNode)) {
	fn(n)
	for _, v := range n.vals {
		v.walk(fn)
	}
}

// requireNoRepeatedKeys fails if any object in n repeats a key exactly.
func requireNoRepeatedKeys(t *testing.T, n *jsonNode, s string) {
	t.Helper()
	n.walk(func(n *jsonNode) {
		seen := map[string]bool{}
		for _, k := range n.keys {
			require.False(t, seen[k], "repeated key %q in %s", k, s)
			seen[k] = true
		}
	})
}

// requireNoLoneSurrogate fails if a string in n holds U+FFFD that the JSON text s does not spell, which means
// the decoder replaced a lone surrogate escape. s must be valid UTF-8.
func requireNoLoneSurrogate(t *testing.T, n *jsonNode, s string) {
	t.Helper()
	if strings.Contains(s, "\uFFFD") || strings.Contains(strings.ToLower(s), `\ufffd`) {
		return
	}
	n.walk(func(n *jsonNode) {
		for _, k := range n.keys {
			require.NotContains(t, k, "\uFFFD", "lone surrogate in %s", s)
		}
		require.NotContains(t, n.str, "\uFFFD", "lone surrogate in %s", s)
	})
}

// requireForwardMemoUnambiguous checks that a memo the filter accepted, if packet-forward-middleware would
// forward it, decodes the same way under any reasonable JSON decoder and gives the middleware well-defined
// work: no repeated or case-variant keys where encoding/json matches keys without case, depth and length
// within the limits, valid UTF-8 without lone surrogates, metadata that decodes and validates, an integer
// timeout in the int64 range, and a next that re-encodes to a JSON object within the limits.
func requireForwardMemoUnambiguous(t *testing.T, memo string) {
	t.Helper()
	// This is how packet-forward-middleware decides whether to forward.
	d := map[string]any{}
	if json.Unmarshal([]byte(memo), &d) != nil || d["forward"] == nil {
		return
	}
	require.LessOrEqual(t, len(memo), transfertypes.MaximumMemoLength)
	require.True(t, utf8.ValidString(memo), "invalid UTF-8 in %q", memo)
	root := decodeJSONNode(t, memo)
	require.LessOrEqual(t, root.depth, maxPFMForwardMemoDepth, memo)
	requireNoRepeatedKeys(t, root, memo)
	requireNoLoneSurrogate(t, root, memo)

	// encoding/json matches struct fields without case, so a case variant of a field name would compete with it.
	var fwd *jsonNode
	for i, k := range root.keys {
		if strings.EqualFold(k, "forward") {
			require.Equal(t, "forward", k, memo)
			fwd = root.vals[i]
		}
	}
	require.NotNil(t, fwd, memo)
	require.Equal(t, byte('{'), fwd.kind, "forward is not an object: %s", memo)
	for _, k := range fwd.keys {
		for _, name := range forwardFields {
			if strings.EqualFold(k, name) {
				require.Equal(t, name, k, memo)
			}
		}
	}

	// The middleware decodes and validates the metadata.
	var m pfmtypes.PacketMetadata
	require.NoError(t, json.Unmarshal([]byte(memo), &m), memo)
	require.NotNil(t, m.Forward, memo)
	require.NoError(t, m.Forward.Validate(), memo)

	var raw struct {
		Forward map[string]json.RawMessage `json:"forward"`
	}
	require.NoError(t, json.Unmarshal([]byte(memo), &raw), memo)
	if timeout, ok := raw.Forward["timeout"]; ok {
		// pfmtypes.Duration converts a numeric timeout from float64; outside the int64 range or with a
		// fraction, the result depends on the platform or rounding.
		var v any
		require.NoError(t, json.Unmarshal(timeout, &v))
		switch v := v.(type) {
		case float64:
			require.True(t, v >= -0x1p63 && v < 0x1p63 && v == math.Trunc(v), "timeout %s in %s", timeout, memo)
		case string:
			_, err := time.ParseDuration(v)
			require.NoError(t, err, memo)
		default:
			require.Failf(t, "timeout is not a number or a string", "%s", memo)
		}
	}
	next, ok := raw.Forward["next"]
	if !ok || string(next) == "null" {
		return
	}
	switch next[0] {
	case '{':
	case '"':
		// The middleware sends a string next as the next memo. It must be a JSON object within the limits.
		var s string
		require.NoError(t, json.Unmarshal(next, &s))
		require.True(t, json.Valid([]byte(s)), "next %q is not JSON in %s", s, memo)
		require.True(t, strings.HasPrefix(strings.TrimLeft(s, " \t\r\n"), "{"), "next %q is not an object in %s", s, memo)
		inner := decodeJSONNode(t, s)
		require.LessOrEqual(t, inner.depth, maxPFMForwardMemoDepth, memo)
		requireNoRepeatedKeys(t, inner, s)
		requireNoLoneSurrogate(t, inner, s)
	default:
		require.Failf(t, "next is not null, an object or a string", "%s", memo)
	}
	// The middleware encodes next as the next memo. That must work and give the same bytes every time.
	require.NotNil(t, m.Forward.Next)
	nextMemo, err := json.Marshal(m.Forward.Next)
	require.NoError(t, err, memo)
	again, err := json.Marshal(m.Forward.Next)
	require.NoError(t, err)
	require.Equal(t, nextMemo, again)
	require.True(t, json.Valid(nextMemo), memo)
	require.Equal(t, byte('{'), nextMemo[0], "next memo %s from %s", nextMemo, memo)
	require.LessOrEqual(t, jsonDepth(t, string(nextMemo)), maxPFMForwardMemoDepth, memo)
}

// fuzzForwardMemoSeeds returns memos that cover the edge cases in deterministic_msgs_test.go.
func fuzzForwardMemoSeeds() []string {
	fwd := forwardMemo
	head := forwardMemo(``)[:len(forwardMemo(``))-1]
	return []string{
		``, `hello`, `null`, `[]`, `{}`, `{"forward":null}`, `{"forward":{}}`, `[{"forward":{}}]`,
		`{"forward":{"receiver":"x"`, `{"wasm":{"contract":"c","msg":{}}}`,
		fwd(``),
		fwd(`,"timeout":"10m"`), fwd(`,"timeout":600000000000`), fwd(`,"timeout":6e11`), fwd(`,"timeout":-1`),
		fwd(`,"timeout":9223372036854774784`), fwd(`,"timeout":-9223372036854775808`),
		fwd(`,"timeout":9223372036854775807`), fwd(`,"timeout":9223372036854775808`), fwd(`,"timeout":9.3e18`),
		fwd(`,"timeout":1e19`), fwd(`,"timeout":-1e19`), fwd(`,"timeout":1e400`), fwd(`,"timeout":1.5`),
		fwd(`,"timeout":0.` + strings.Repeat("0", 100) + `1`), fwd(`,"timeout":"9999999999h"`), fwd(`,"timeout":"abc"`),
		fwd(`,"timeout":true`), fwd(`,"timeout":null`), fwd(`,"timeout":{}`), fwd(`,"timeout":"1\u006d"`),
		fwd(`,"retries":0`), fwd(`,"retries":255`), fwd(`,"retries":256`), fwd(`,"retries":-1`), fwd(`,"retries":1e0`),
		fwd(`,"retries":"1"`), fwd(`,"retries":null`), fwd(`,"retries":` + strings.Repeat("9", 40)),
		fwd(`,"next":null`), fwd(`,"next":5`), fwd(`,"next":[]`), fwd(`,"next":""`), fwd(`,"next":"null"`),
		fwd(`,"next":"abc"`), fwd(`,"next":"[1,2]"`), fwd(`,"next":"\"{}\""`), fwd(`,"next":"{\"a\":"`),
		fwd(`,"next":{"forward":{"receiver":"osmo1r","port":"transfer","channel":"channel-2","timeout":"5m","next":{"forward":{"receiver":"noble1r","port":"transfer","channel":"channel-9"}}}}`),
		fwd(`,"next":"{\"forward\":{\"receiver\":\"osmo1r\",\"port\":\"transfer\",\"channel\":\"channel-2\"}}"`),
		fwd(`,"next":"{\"forward\":{\"receiver\":\"osmo1r\",\"port\":\"transfer\",\"channel\":\"channel-2\",\"next\":\"{\\\"forward\\\":{\\\"receiver\\\":\\\"r\\\",\\\"port\\\":\\\"transfer\\\",\\\"channel\\\":\\\"channel-3\\\"}}\"}}"`),
		fwd(`,"next":{"wasm":{"contract":"osmo1c","msg":{"swap":{"min":"1"}}}}`),
		fwd(`,"next":{"a":1,"a":2}`), fwd(`,"next":{"a":1,"A":2}`), fwd(`,"next":{"x":{"a":1},"y":{"a":1}}`),
		fwd(`,"next":"{\"a\":1,\"a\":2}"`), fwd(`,"next":{"a":1},"next":{}`), fwd(`,"Next":{"a":1},"next":{"b":2}`),
		fwd(`,"next":{"n":123456789012345678901234567890,"f":1e400,"e":-0.0e-0}`),
		fwd(`,"next":{"a":"\ud83d\ude00"}`), fwd(`,"next":{"a":"\ud800"}`), fwd(`,"next":"{\"a\":\"\\ud800\"}"`),
		fwd(`,"next":"{\"a\":\"\\ud83d\\ude00\"}"`), fwd(`,"next":{"<>&":"\u2028\u2029"}`),
		fwd(`,"next":` + nested(maxPFMForwardMemoDepth-2)), fwd(`,"next":` + nested(maxPFMForwardMemoDepth-1)),
		fwd(`,"next":` + jsonString(nestedJSON(maxPFMForwardMemoDepth))), fwd(`,"next":` + jsonString(nestedJSON(maxPFMForwardMemoDepth+1))),
		fwd(`,"memo":"x"`), fwd(`,"time_out":"1m"`), fwd(`,"Port":"transfer"`), fwd(`,"retrieſ":1`), fwd(`,"receiver":"y"`),
		head + `,"a":1,"a":2}`, head + `,"a":1,"A":2}`, head + `,"other":{"a":1,"a":2}}`, head + `,"wasm":{}}`,
		`{"forward":{"receiver":"","port":"transfer","channel":"channel-7"}}`,
		`{"forward":{"port":"transfer","channel":"channel-7"}}`,
		`{"forward":{"receiver":"x","port":"a/b","channel":"channel-7"}}`,
		`{"forward":{"receiver":"x","port":"transfer","channel":"notachannel"}}`,
		`{"forward":{"receiver":5,"port":"transfer","channel":"channel-7"}}`,
		`{"forward":5}`, `{"forward":[]}`, `{"forward":"abc"}`, `{"forward":true}`,
		`{"forward":{"receiver":"x","port":"transfer","channel":"channel-7"},"forward":{"receiver":"x","port":"transfer","channel":"channel-7"}}`,
		`{"forward":{"receiver":"x","port":"transfer","channel":"channel-7"},"FORWARD":null}`,
		`{"forward":null,"forward":{"receiver":"x","port":"transfer","channel":"channel-7"}}`,
		`{"forward":null,"Forward":{"receiver":"x","port":"a/b","channel":"channel-7"}}`,
		`{"Forward":{"receiver":"x","port":"transfer","channel":"channel-7"}}`,
		`{"for_ward":{"receiver":"x","port":"transfer","channel":"channel-7"}}`,
		`{"\u0066orward":{"receiver":"x","\u0070ort":"transfer","channel":"channel-7"}}`,
		`{"forward":{"receiver":"x\ty\/z\"\\","port":"transfer","channel":"channel-7"}}`,
		`{"forward":{"receiver":"\ud83d\ude00","port":"transfer","channel":"channel-7"}}`,
		`{"forward":{"receiver":"\udc00","port":"transfer","channel":"channel-7"}}`,
		`{"forward":{"receiver":"\ude00\ud83d","port":"transfer","channel":"channel-7"}}`,
		`{"forward":{"receiver":"\ufffd","port":"transfer","channel":"channel-7"}}`,
		"{\"forward\":{\"receiver\":\"\xff\",\"port\":\"transfer\",\"channel\":\"channel-7\"}}",
		"{\"forward\":{\"receiver\":\"\xed\xa0\x80\",\"port\":\"transfer\",\"channel\":\"channel-7\"}}",
		` { "forward" : { "receiver" : "x" , "port" : "transfer" , "channel" : "channel-7" } } `,
		`{"forward":{"receiver":"x","port":"transfer","channel":"channel-7"}} trailing`,
		`{"forward":{"receiver":"x","port":"transfer","channel":"channel-7","next":` + strings.Repeat(`{"a":`, 200),
		nested(10 * maxPFMForwardMemoDepth),
		pfmForwardMemo(maxPFMForwardMemoDepth, 0), pfmForwardMemo(maxPFMForwardMemoDepth+1, 0),
		pfmDeepStringNextMemo(maxPFMForwardMemoDepth + 1),
	}
}

// fuzzSource hands out the bytes of a fuzz input, then zeros.
type fuzzSource struct {
	b []byte
	i int
}

func (s *fuzzSource) byte() byte {
	if s.i >= len(s.b) {
		return 0
	}
	s.i++
	return s.b[s.i-1]
}

// intn returns a number in [0, n).
func (s *fuzzSource) intn(n int) int {
	return int(s.byte()) % n
}

func pick[T any](s *fuzzSource, options ...T) T {
	return options[s.intn(len(options))]
}

// genValue is a generated JSON value: a genObject or raw JSON text.
type genValue interface {
	json(s *fuzzSource) string
}

// genRaw is raw JSON text.
type genRaw string

func (r genRaw) json(*fuzzSource) string { return string(r) }

// genMember is an object member. name is the decoded key; rawKey is its JSON text inside the quotes.
type genMember struct {
	name, rawKey string
	value        genValue
}

// genObject is a JSON object that keeps member order and may repeat keys.
type genObject struct {
	members []genMember
}

func (o *genObject) json(s *fuzzSource) string {
	ws := func() string { return pick(s, "", "", "", " ", "\n\t ") }
	var sb strings.Builder
	sb.WriteString("{" + ws())
	for i, m := range o.members {
		if i > 0 {
			sb.WriteString("," + ws())
		}
		sb.WriteString(`"` + m.rawKey + `"` + ws() + ":" + ws() + m.value.json(s) + ws())
	}
	sb.WriteString("}")
	return sb.String()
}

// add appends a member whose key is name, with one letter possibly spelled as a \u escape.
func (o *genObject) add(s *fuzzSource, name string, value genValue) {
	rawKey := name
	if i := s.intn(4 * len(name)); i < len(name) {
		rawKey = name[:i] + fmt.Sprintf(`\u%04x`, name[i]) + name[i+1:]
	}
	o.members = append(o.members, genMember{name: name, rawKey: rawKey, value: value})
}

// set replaces the value of the first member named name, or adds one.
func (o *genObject) set(s *fuzzSource, name string, value genValue) {
	for i := range o.members {
		if o.members[i].name == name {
			o.members[i].value = value
			return
		}
	}
	o.add(s, name, value)
}

func (o *genObject) get(name string) genValue {
	for _, m := range o.members {
		if m.name == name {
			return m.value
		}
	}
	return nil
}

// shuffle reorders the members.
func (o *genObject) shuffle(s *fuzzSource) {
	for i := len(o.members) - 1; i > 0; i-- {
		j := s.intn(i + 1)
		o.members[i], o.members[j] = o.members[j], o.members[i]
	}
}

// genStringNext is a string next that holds the JSON text of an inner memo.
type genStringNext struct{ memo *genObject }

func (n genStringNext) json(s *fuzzSource) string { return jsonString(n.memo.json(s)) }

// genForwardMemo returns an honest forward memo. stringNexts bounds how many string nexts nest inside it.
func genForwardMemo(s *fuzzSource, stringNexts int) *genObject {
	root := &genObject{}
	root.add(s, "forward", genForward(s, 2, stringNexts))
	if s.intn(3) == 0 {
		root.add(s, "wasm", genRaw(`{"contract":"c","msg":{}}`))
	}
	if s.intn(3) == 0 {
		root.add(s, "callback", genRaw(`"x\u00e9<>&"`))
	}
	root.shuffle(s)
	return root
}

// genForward returns valid forward metadata in an object at the given depth.
func genForward(s *fuzzSource, depth, stringNexts int) *genObject {
	fwd := &genObject{}
	var receiver strings.Builder
	for range 1 + s.intn(4) {
		receiver.WriteString(pick(s, `cosmos1r`, `a`, `\u00e9`, `é`, `\ud83d\ude00`, `😀`, `\"`, `\\`, `\/`, `<>&`, `\ufffd`, `\n`))
	}
	fwd.add(s, "receiver", genRaw(`"`+receiver.String()+`"`))
	fwd.add(s, "port", genRaw(pick(s, `"transfer"`, `"tr"`, `"wasm.osmo1abc"`, `"icahost"`, `"t\u0072ansfer"`)))
	fwd.add(s, "channel", genRaw(fmt.Sprintf(`"channel-%d"`, s.byte())))
	switch s.intn(5) {
	case 1:
		fwd.add(s, "timeout", genRaw(pick(s, `"10m"`, `"1h30m"`, `"1ns"`, `"0s"`, `"-5m"`, `"1\u006d"`)))
	case 2:
		fwd.add(s, "timeout", genRaw(pick(s, `0`, `-1`, `6e11`, `600000000000`, `1.0`, `-9223372036854775808`, `9223372036854774784`, fmt.Sprintf("%d", uint64(s.byte())<<s.intn(53)))))
	}
	switch s.intn(4) {
	case 1:
		fwd.add(s, "retries", genRaw(fmt.Sprintf("%d", s.byte())))
	case 2:
		fwd.add(s, "retries", genRaw(`null`))
	}
	switch s.intn(5) {
	case 1:
		fwd.add(s, "next", genRaw(`null`))
	case 2:
		// The next object sits one level below fwd and its forward object two.
		if depth+2 <= maxPFMForwardMemoDepth {
			next := &genObject{}
			next.add(s, "forward", genForward(s, depth+2, stringNexts))
			fwd.add(s, "next", next)
		}
	case 3:
		if stringNexts > 0 {
			fwd.add(s, "next", genStringNext{genForwardMemo(s, stringNexts-1)})
		}
	case 4:
		if depth+5 <= maxPFMForwardMemoDepth {
			fwd.add(s, "next", genRaw(`{"wasm":{"contract":"osmo1c","msg":{"swap":{"min":"1","route":[1,2.5,"x",null,true]}}}}`))
		}
	}
	fwd.shuffle(s)
	return fwd
}

// numMemoMutations is the number of mutations mutateForwardMemo knows.
const numMemoMutations = 14

// mutateForwardMemo applies mutation m to the honest memo root so that the filter must reject it. It reports
// whether the mutated memo is valid UTF-8, so that the packet-level check must reject it too.
func mutateForwardMemo(s *fuzzSource, root *genObject, m int) (packetLevel bool) {
	fwd := root.get("forward").(*genObject)
	switch m {
	case 0: // Repeat a forward field.
		dup := fwd.members[s.intn(len(fwd.members))]
		fwd.members = append(fwd.members, dup)
		fwd.shuffle(s)
	case 1: // A case variant of the forward key.
		root.add(s, pick(s, "Forward", "FORWARD", "forwarD"), pick[genValue](s, genRaw(`{}`), genRaw(`null`), genRaw(`5`), genForward(s, 2, 0)))
		root.shuffle(s)
	case 2: // A case variant of a forward field.
		key := pick(s, "Receiver", "PORT", "chAnnel", "Timeout", "retrieſ", "Retries", "NEXT", "Next")
		fwd.members = append(fwd.members, genMember{name: key, rawKey: key, value: genRaw(pick(s, `"x"`, `1`, `null`, `{}`))})
		fwd.shuffle(s)
	case 3: // A timeout that is not an integer in the int64 range, or not a duration.
		fwd.set(s, "timeout", genRaw(pick(s, `1.5`, `1e19`, `-1e19`, `9223372036854775807`, `9223372036854775808`, `9.3e18`,
			`-9223372036854777856`, `1e400`, `0.5e-3`, `true`, `null`, `{}`, `[]`, `"abc"`, `"9999999999h"`, `""`, `"10"`)))
	case 4: // A next that is not null, an object, or a string holding a JSON object within the limits.
		fwd.set(s, "next", genRaw(pick(s, `5`, `true`, `false`, `[]`, `[{"a":1}]`, `"null"`, `"5"`, `"[1]"`, `"abc"`, `""`, `" "`,
			`"\"{}\""`, `"{\"a\":1,\"a\":2}"`, `"{\"a\":"`, `"{\"a\":\"\\ud800\"}"`, jsonString(nestedJSON(maxPFMForwardMemoDepth+1+s.intn(8))))))
	case 5: // Retries that do not fit uint8.
		fwd.set(s, "retries", genRaw(pick(s, `256`, `-1`, `1.5`, `"1"`, `1e0`, `true`, `{}`, strings.Repeat("9", 30))))
	case 6: // Nest too deep.
		fwd.set(s, "next", genRaw(nested(maxPFMForwardMemoDepth-1+s.intn(30))))
	case 7: // A lone surrogate.
		fwd.set(s, "receiver", genRaw(pick(s, `"\ud800"`, `"x\udc00"`, `"\ude00\ud83d"`, `"\uDBFF"`, `"\ud800\u0041"`)))
	case 8: // Metadata that fails validation.
		switch s.intn(3) {
		case 0:
			fwd.set(s, "receiver", genRaw(pick(s, `""`, `5`, `null`, `{}`)))
		case 1:
			fwd.set(s, "port", genRaw(pick(s, `""`, `"a/b"`, `"a"`, `"tr@nsfer"`, `true`, jsonString(strings.Repeat("p", 200)))))
		default:
			fwd.set(s, "channel", genRaw(pick(s, `""`, `"channel/1"`, `null`, `7`)))
		}
	case 9: // A repeated top-level key, exactly or in another case.
		root.add(s, "x", genRaw(`1`))
		root.add(s, pick(s, "x", "X"), genRaw(`2`))
		root.shuffle(s)
	case 10: // A repeated key inside next.
		next := &genObject{}
		next.add(s, "a", genRaw(`1`))
		next.add(s, "forward", genForward(s, 4, 0))
		next.add(s, "a", genRaw(`{}`))
		next.shuffle(s)
		fwd.set(s, "next", next)
	case 11: // Too long.
		fwd.set(s, "receiver", genRaw(`"`+strings.Repeat("r", transfertypes.MaximumMemoLength)+`"`))
	case 12: // Invalid UTF-8.
		fwd.set(s, "receiver", genRaw("\"r\xff\""))
		return false
	case 13: // forward is not an object.
		root.set(s, "forward", genRaw(pick(s, `5`, `"abc"`, `[]`, `true`, `[{"receiver":"x"}]`)))
	}
	return true
}
