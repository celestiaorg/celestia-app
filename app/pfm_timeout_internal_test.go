package app

import (
	"encoding/json"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"cosmossdk.io/log"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	pfmtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

func transferPacketData(t *testing.T, memo string) []byte {
	t.Helper()
	data, err := json.Marshal(ibctransfertypes.FungibleTokenPacketData{
		Denom: "uatom", Amount: "1", Sender: "remote", Receiver: "pfm", Memo: memo,
	})
	require.NoError(t, err)
	return data
}

// TestRejectPFMForwardMemo checks which forward memos the wrapper rejects
// before packet-forward-middleware runs: a numeric timeout outside the int64
// range, a memo that has a "forward" key but decodes to no forward
// metadata, and a forward memo with a repeated key.
func TestRejectPFMForwardMemo(t *testing.T) {
	testCases := []struct {
		name string
		memo string
		want bool
	}{
		{"no memo", ``, false},
		{"memo without forward", `{"wasm":{"timeout":1e19}}`, false},
		{"invalid memo json", `{"forward":{"timeout":1e19}`, false},
		{"forward null", `{"forward":null}`, false},
		{"forward not an object", `{"forward":5}`, false},
		{"no timeout", `{"forward":{"receiver":"r"}}`, false},
		{"string timeout", `{"forward":{"timeout":"10m"}}`, false},
		{"invalid string timeout", `{"forward":{"timeout":"1e19"}}`, false},
		{"bool timeout", `{"forward":{"timeout":true}}`, false},
		{"null timeout", `{"forward":{"timeout":null}}`, false},
		{"zero", `{"forward":{"timeout":0}}`, false},
		{"negative zero", `{"forward":{"timeout":-0}}`, false},
		{"fraction", `{"forward":{"timeout":1.5}}`, false},
		{"negative in range", `{"forward":{"timeout":-5}}`, false},
		{"min int64", `{"forward":{"timeout":-9223372036854775808}}`, false},
		{"largest float64 below 2^63", `{"forward":{"timeout":9223372036854774784}}`, false},
		{"max int64 literal rounds to 2^63", `{"forward":{"timeout":9223372036854775807}}`, true},
		{"2^63", `{"forward":{"timeout":9223372036854775808}}`, true},
		{"9.3e18", `{"forward":{"timeout":9.3e18}}`, true},
		{"1e19", `{"forward":{"timeout":1e19}}`, true},
		{"-1e19", `{"forward":{"timeout":-1e19}}`, true},
		{"next float64 below -2^63", `{"forward":{"timeout":-9223372036854777856}}`, true},
		{"does not fit in float64", `{"forward":{"timeout":1e400}}`, false},
		{"field name case", `{"forward":{"TimeOut":1e19}}`, true},
		{"forward key case", `{"forward":{},"Forward":{"timeout":1e19}}`, true},
		{"duplicate timeout, last in range", `{"forward":{"timeout":1e19,"timeout":5}}`, true},
		{"duplicate timeout, last out of range", `{"forward":{"timeout":5,"timeout":1e19}}`, true},
		{"timeout in next only", `{"forward":{"next":{"forward":{"timeout":1e19}}}}`, false},
		{"timeout in next string only", `{"forward":{"next":"{\"forward\":{\"timeout\":1e19}}"}}`, false},
		{"upper case exponent", `{"forward":{"timeout":1E19}}`, true},
		{"positive exponent sign", `{"forward":{"timeout":1e+19}}`, true},
		{"2^63 as integer digits", `{"forward":{"timeout":10000000000000000000}}`, true},
		{"1e308", `{"forward":{"timeout":1e308}}`, true},
		{"negative zero fraction", `{"forward":{"timeout":-0.0}}`, false},
		{"escaped field name", `{"forward":{"timeout":1e19}}`, true},
		{"array timeout", `{"forward":{"timeout":[1e19]}}`, false},
		{"object timeout", `{"forward":{"timeout":{"v":1e19}}}`, false},
		{"stringified forward", `{"forward":"{\"timeout\":1e19}"}`, false},
		{"duplicate forward merges", `{"forward":{"timeout":1e19},"forward":{"receiver":"r"}}`, true},
		{"duplicate forward, last timeout in range", `{"forward":{"timeout":1e19},"forward":{"timeout":5}}`, true},
		{"upper case forward first, lower case second", `{"Forward":{"timeout":1e19},"forward":{"receiver":"r"}}`, true},
		{"forward reset by null", `{"forward":{"timeout":1e19},"Forward":null,"forward":{"receiver":"r"}}`, true},
		{"lower case forward key null", `{"Forward":{"timeout":1e19},"forward":null}`, false},
		{"duplicate timeout, last does not fit in float64", `{"forward":{"timeout":5,"timeout":1e400}}`, false},
		{"duplicate timeout, last null", `{"forward":{"timeout":1e19,"timeout":null}}`, true},
		{"duplicate timeout, last string", `{"forward":{"timeout":1e19,"timeout":"10m"}}`, true},
		{"out of range with another invalid field", `{"forward":{"timeout":1e19,"retries":300}}`, true},
		// A memo with a non-null "forward" key is a forward memo. If the
		// forward metadata decodes to nil, or a key repeats, the memo is
		// rejected.
		{"Forward null after forward", `{"forward":{"receiver":"r"},"Forward":null}`, true},
		{"FORWARD null after forward", `{"forward":{"receiver":"r"},"FORWARD":null}`, true},
		{"escaped Forward null after forward", `{"forward":{"receiver":"r"},"Forw\u0061rd":null}`, true},
		{"Forward null after empty forward", `{"forward":{},"Forward":null}`, true},
		{"Forward null before forward", `{"Forward":null,"forward":{"receiver":"r"}}`, true},
		{"forward object then forward null", `{"forward":{"receiver":"r"},"forward":null}`, false},
		{"escaped forward null after forward", `{"forward":{"receiver":"r"},"forw\u0061rd":null}`, false},
		{"Forward object then forward null", `{"Forward":{"receiver":"r"},"forward":null}`, false},
		{"Forward null then invalid Forward", `{"forward":{"receiver":"r"},"Forward":null,"Forward":5}`, true},
		{"Forward null then Forward object", `{"forward":{"receiver":"r"},"Forward":null,"Forward":{"receiver":"r"}}`, true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, rejectPFMForwardMemo(transferPacketData(t, tc.memo)))
		})
	}

	t.Run("not transfer packet data", func(t *testing.T) {
		for _, data := range [][]byte{nil, {}, []byte("not json"), []byte(`{"forward":{"timeout":1e19}}`), {0xff, 0x00}} {
			require.False(t, rejectPFMForwardMemo(data))
		}
	})
}

// TestPFMIBCMiddlewareRejectsForwardMemo checks that the wrapper answers
// before packet-forward-middleware runs. The wrapped middleware is a zero
// value, so calling it would panic.
func TestPFMIBCMiddlewareRejectsForwardMemo(t *testing.T) {
	for _, memo := range []string{
		`{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","timeout":1e19}}`,
		`{"forward":{"receiver":"r","port":"transfer","channel":"channel-0"},"Forward":null}`,
	} {
		ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())
		packet := channeltypes.Packet{Data: transferPacketData(t, memo)}
		ack := pfmIBCMiddleware{}.OnRecvPacket(ctx, packet, nil)
		require.False(t, ack.Success())
		require.Equal(t, `{"error":"ABCI code: 11151: error handling packet: see events for details"}`, string(ack.Acknowledgement()))
		require.Empty(t, ctx.EventManager().Events())
	}
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

// TestRejectPFMForwardMemoLimits checks the bounds on the nesting depth and
// the length of a forward memo, at the limits and one past them.
func TestRejectPFMForwardMemoLimits(t *testing.T) {
	require.Len(t, pfmForwardMemo(2, ibctransfertypes.MaximumMemoLength), ibctransfertypes.MaximumMemoLength)

	testCases := []struct {
		name string
		memo string
		want bool
	}{
		{"depth 2", pfmForwardMemo(2, 0), false},
		{"depth at limit", pfmForwardMemo(maxPFMForwardMemoDepth, 0), false},
		{"depth over limit", pfmForwardMemo(maxPFMForwardMemoDepth+1, 0), true},
		{"depth 4000", pfmForwardMemo(4000, 0), true},
		{"array depth over limit", `{"forward":{"receiver":"r","next":` + strings.Repeat(`[`, maxPFMForwardMemoDepth-1) + strings.Repeat(`]`, maxPFMForwardMemoDepth-1) + `}}`, true},
		{"brackets in strings do not count", `{"forward":{"receiver":"` + strings.Repeat(`{[`, 100) + `\"{"}}`, false},
		{"length at limit", pfmForwardMemo(2, ibctransfertypes.MaximumMemoLength), false},
		{"length over limit", pfmForwardMemo(2, ibctransfertypes.MaximumMemoLength+1), true},
		{"length and depth at limit", pfmForwardMemo(maxPFMForwardMemoDepth, ibctransfertypes.MaximumMemoLength), false},
		{"length over limit with invalid forward", `{"forward":{"receiver":"` + strings.Repeat("r", ibctransfertypes.MaximumMemoLength) + `","retries":300}}`, true},
		// A string next is the memo of the forwarded packet, which
		// packet-forward-middleware decodes again if that packet times out.
		{"string next at depth limit", pfmForwardMemoWithNext(jsonString(nestedJSON(maxPFMForwardMemoDepth)), 0), false},
		{"string next over depth limit", pfmForwardMemoWithNext(jsonString(nestedJSON(maxPFMForwardMemoDepth+1)), 0), true},
		{"string next 3000 levels", pfmForwardMemoWithNext(jsonString(nestedJSON(3000)), 0), true},
		{"string next array over depth limit", pfmForwardMemoWithNext(jsonString(strings.Repeat(`[`, maxPFMForwardMemoDepth+1)+strings.Repeat(`]`, maxPFMForwardMemoDepth+1)), 0), true},
		{"string next with repeated key", pfmForwardMemoWithNext(jsonString(`{"a":1,"a":2}`), 0), true},
		{"string next with repeated nested key", pfmForwardMemoWithNext(jsonString(`{"a":{"b":1,"b":2}}`), 0), true},
		{"string next holding a JSON string", pfmForwardMemoWithNext(jsonString(jsonString(`{"a":1}`)), 0), true},
		{"string next, not JSON", pfmForwardMemoWithNext(jsonString(`{"a":`), 0), false},
		// A string next that holds JSON must hold an object, as an honest
		// memo does. A string next that is not JSON is left to
		// packet-forward-middleware, which fails to encode it.
		{"string next, number", pfmForwardMemoWithNext(jsonString(`5`), 0), true},
		{"string next, null", pfmForwardMemoWithNext(jsonString(`null`), 0), true},
		{"string next, true", pfmForwardMemoWithNext(jsonString(`true`), 0), true},
		{"string next, array", pfmForwardMemoWithNext(jsonString(`[{"a":1}]`), 0), true},
		{"string next, object", pfmForwardMemoWithNext(jsonString(` {"a":[1]} `), 0), false},
		{"next null", pfmForwardMemoWithNext(`null`, 0), false},
		{"next number", pfmForwardMemoWithNext(`5`, 0), true},
		{"next true", pfmForwardMemoWithNext(`true`, 0), true},
		{"next array", pfmForwardMemoWithNext(`[{"a":1}]`, 0), true},
		{"Next key case, next array", `{"forward":{"receiver":"r","Next":[]}}`, true},
		{"next key in a nested object is not forward.next", `{"forward":{"receiver":"r","next":{"next":[1]}},"a":{"next":5}}`, false},
		{"Next key case, string next over depth limit", `{"forward":{"receiver":"r","NEXT":` + jsonString(nestedJSON(maxPFMForwardMemoDepth+1)) + `}}`, true},
		// Memos that packet-forward-middleware does not forward are not
		// bounded here.
		{"deep memo without forward", `{"a":` + strings.Repeat(`{"a":`, 4000) + `1` + strings.Repeat(`}`, 4001), false},
		{"long memo without forward", `{"a":"` + strings.Repeat("r", 2*ibctransfertypes.MaximumMemoLength) + `"}`, false},
		{"repeated keys without forward", `{"a":1,"a":2,"forward":null}`, false},
		{"deep forward null", `{"forward":null,"a":` + strings.Repeat(`[`, 100) + strings.Repeat(`]`, 100) + `}`, false},
		// Honest multi-hop memos.
		{"1-hop", `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","timeout":"10m","retries":2}}`, false},
		{"2-hop", `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","next":{"forward":{"receiver":"r2","port":"transfer","channel":"channel-1"}}}}`, false},
		{"2-hop next string", `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","next":"{\"forward\":{\"receiver\":\"r2\",\"port\":\"transfer\",\"channel\":\"channel-1\"}}"}}`, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, rejectPFMForwardMemo(transferPacketData(t, tc.memo)))
		})
	}
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

// pfmMemoWork returns the bytes that packet-forward-middleware allocates to
// decode memo and encode its forward.next value again, as it does before it
// forwards the packet, and then to decode and encode that forwarded memo
// again, as it does when the forwarded packet times out and is retried. The
// allocation grows with the work.
func pfmMemoWork(t *testing.T, memo string) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var m pfmtypes.PacketMetadata
	if err := json.Unmarshal([]byte(memo), &m); err == nil && m.Forward != nil && m.Forward.Next != nil {
		if fwdMemo, err := json.Marshal(m.Forward.Next); err == nil {
			var next pfmtypes.JSONObject
			if err := json.Unmarshal(fwdMemo, &next); err == nil {
				_, _ = json.Marshal(next)
			}
		}
	}
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestPFMForwardMemoWorkIsBounded checks that the wrapper rejects, before
// packet-forward-middleware runs, forward memos whose decoding and encoding
// work grows faster than their length, and that the work for the worst
// accepted memos stays under a bound that is a small multiple of the
// largest memo.
func TestPFMForwardMemoWorkIsBounded(t *testing.T) {
	// The forwarded memo can be six times longer than the memo, because
	// json.Marshal escapes '<', '>' and '&' in six bytes and prints a short
	// number such as 1e20 in full. The work reads each byte of it at every
	// nesting level, a few times.
	const workBound = 4 * maxPFMForwardMemoDepth * 6 * ibctransfertypes.MaximumMemoLength

	// Repeated next keys under the length limit and at depth 3.
	var keys strings.Builder
	for i := range 2000 {
		if i > 0 {
			keys.WriteByte(',')
		}
		keys.WriteString(jsonString(string(rune('A'+i/64)) + string(rune('A'+i%64))))
		keys.WriteString(`:0`)
	}
	repeatedNext := `{"forward":{"next":{` + keys.String() + `}` + strings.Repeat(`,"next":{}`, 1800) + `}}`
	// Many distinct keys, then the same keys again, inside next.
	var dupKeys strings.Builder
	for r := range 2 {
		for i := range 1500 {
			if r > 0 || i > 0 {
				dupKeys.WriteByte(',')
			}
			dupKeys.WriteString(jsonString(strconv.Itoa(i)))
			dupKeys.WriteString(`:0`)
		}
	}
	amplified := map[string]string{
		"nested next":        pfmForwardMemo(4000, 0),
		"nested string next": pfmForwardMemoWithNext(jsonString(nestedJSON(3900)), 0),
		"repeated next keys": repeatedNext,
		// Its cost is in moving keys, not in allocation.
		"repeated keys in next": pfmForwardMemoWithNext(`{`+dupKeys.String()+`}`, 0),
	}
	// The wrapped middleware is a zero value, so calling it would panic.
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())
	for name, memo := range amplified {
		require.LessOrEqual(t, len(memo), ibctransfertypes.MaximumMemoLength, name)
		if name != "repeated keys in next" {
			work := pfmMemoWork(t, memo)
			t.Logf("rejected %s: %d bytes allocated", name, work)
			require.Greater(t, work, uint64(workBound), "%s must cost more than the bound", name)
		}
		ack := pfmIBCMiddleware{}.OnRecvPacket(ctx, channeltypes.Packet{Data: transferPacketData(t, memo)}, nil)
		require.Equal(t, `{"error":"ABCI code: 11151: error handling packet: see events for details"}`, string(ack.Acknowledgement()), name)
	}
	ack := pfmIBCMiddleware{}.OnRecvPacket(ctx, channeltypes.Packet{Data: transferPacketData(t, pfmForwardMemo(2, 1<<20))}, nil)
	require.False(t, ack.Success())

	// The worst accepted memos, at the length and depth limits. In the last
	// one, json.Marshal expands each "<" to six bytes in the forwarded memo.
	deepHTML := func(n int) string {
		return strings.Repeat(`{"a":`, maxPFMForwardMemoDepth-3) + `"` + strings.Repeat("<", n) + `"` + strings.Repeat(`}`, maxPFMForwardMemoDepth-3)
	}
	htmlLen := ibctransfertypes.MaximumMemoLength - len(pfmForwardMemoWithNext(deepHTML(0), 0))
	// encoding/json decodes each number to a float64 and prints 1e20 with
	// 21 digits.
	deepNumbers := func(n int) string {
		return strings.Repeat(`{"a":`, maxPFMForwardMemoDepth-3) + `[1` + strings.Repeat(`,1e20`, n) + `]` + strings.Repeat(`}`, maxPFMForwardMemoDepth-3)
	}
	numbers := (ibctransfertypes.MaximumMemoLength - len(pfmForwardMemoWithNext(deepNumbers(0), 0))) / len(`,1e20`)
	for name, memo := range map[string]string{
		"object next":     pfmForwardMemo(maxPFMForwardMemoDepth, ibctransfertypes.MaximumMemoLength),
		"string next":     pfmForwardMemoWithNext(jsonString(nestedJSON(maxPFMForwardMemoDepth)), ibctransfertypes.MaximumMemoLength),
		"html in next":    pfmForwardMemoWithNext(deepHTML(htmlLen), 0),
		"numbers in next": pfmForwardMemoWithNext(deepNumbers(numbers), ibctransfertypes.MaximumMemoLength),
	} {
		require.Len(t, memo, ibctransfertypes.MaximumMemoLength, name)
		require.False(t, rejectPFMForwardMemo(transferPacketData(t, memo)), name)
		work := pfmMemoWork(t, memo)
		t.Logf("accepted %s: %d bytes allocated", name, work)
		require.Less(t, work, uint64(workBound), name)
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
		}, bz)
	}
}

// TestPFMRetryMemoWithinLimits checks the limits on the memo of a forwarded
// packet that times out, which packet-forward-middleware decodes and encodes
// again to retry it.
func TestPFMRetryMemoWithinLimits(t *testing.T) {
	maxLen := 6 * ibctransfertypes.MaximumMemoLength
	pad := func(n int) string {
		head := `{"forward":{"receiver":"`
		return head + strings.Repeat("r", n-len(head)-3) + `"}}`
	}
	testCases := []struct {
		name string
		memo string
		want bool
	}{
		{"no memo", ``, true},
		{"2-hop", `{"forward":{"receiver":"r2","port":"transfer","channel":"channel-1"}}`, true},
		{"depth at limit", nestedJSON(maxPFMForwardMemoDepth), true},
		{"depth over limit", nestedJSON(maxPFMForwardMemoDepth + 1), false},
		{"depth 2000", nestedJSON(2000), false},
		{"repeated key", `{"a":1,"a":2}`, false},
		{"keys differ in case", `{"a":1,"A":2}`, true},
		{"length at limit", pad(maxLen), true},
		{"length over limit", pad(maxLen + 1), false},
		{"not JSON", `hello`, false},
		{"array", `[1]`, false},
		{"number", `5`, false},
		{"null", `null`, false},
		{"string", `"{}"`, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, pfmRetryMemoWithinLimits(tc.memo))
		})
	}
}

// TestPFMForwardMemoDepthLimitIs16 checks the depth limit at 16 levels and
// one past it, for a forward memo, a string next and a retried memo.
func TestPFMForwardMemoDepthLimitIs16(t *testing.T) {
	for depth, want := range map[int]bool{16: false, 17: true} {
		require.Equal(t, want, rejectPFMForwardMemo(transferPacketData(t, pfmForwardMemo(depth, 0))), "forward memo depth %d", depth)
		require.Equal(t, want, rejectPFMForwardMemo(transferPacketData(t, pfmForwardMemoWithNext(jsonString(nestedJSON(depth)), 0))), "string next depth %d", depth)
		require.Equal(t, !want, pfmRetryMemoWithinLimits(nestedJSON(depth)), "retry memo depth %d", depth)
	}
}
