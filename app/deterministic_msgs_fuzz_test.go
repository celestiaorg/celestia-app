package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	pfmtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// seedMemos returns the memos of the forward memo table tests.
func seedMemos() []string {
	memos := make([]string, 0, 200)
	for _, tc := range append(validateForwardMemoAllowCases(), validateForwardMemoRejectCases()...) {
		memos = append(memos, tc.memo)
	}
	return append(memos, pfmRepeatedNextMemo(), pfmDeepStringNextMemo(3000), pfmForwardMemo(maxPFMForwardMemoDepth, maxPFMForwardMemoLength),
		pfmForwardMemoWithNext(jsonString(nestedJSON(maxPFMForwardMemoDepth+1)), 0), `{"a":1}}`, `[}`, `{"forward":{"NEXT":"\"`, "{\"\xff\":1,\"\xfe\":2}", `1 2`, `{"for-ward":{},"re-ceiver":1,"time_out":2}`)
}

// FuzzValidateForwardMemo checks that validateForwardMemo does not panic, is deterministic, and that
// packet-forward-middleware v8.2.0 handles every memo it accepts as checkPFMForwards describes.
func FuzzValidateForwardMemo(f *testing.F) {
	for _, memo := range seedMemos() {
		f.Add(memo)
	}
	f.Fuzz(func(t *testing.T, memo string) {
		_ = checkForwardMemo(t, memo)
	})
}

// FuzzPFMMemoWithinLimits checks the memo tokenizer and the scanObject key scanner against encoding/json.
func FuzzPFMMemoWithinLimits(f *testing.F) {
	for _, memo := range seedMemos() {
		f.Add([]byte(memo), true)
		f.Add([]byte(memo), false)
	}
	f.Fuzz(func(t *testing.T, memo []byte, forwardMemo bool) {
		before := bytes.Clone(memo)
		got := pfmMemoWithinLimits(memo, forwardMemo)
		require.Equal(t, before, memo, "the input must not change")
		require.Equal(t, got, pfmMemoWithinLimits(memo, forwardMemo))
		s := string(memo)
		// The functions that expect valid JSON must not panic on other input.
		walked := pfmWalkMemo(s, forwardMemo)
		stringNext := pfmStringNextWithinLimits(s)
		pfmForwardMemoWithinLimits(s)
		if !json.Valid(memo) {
			require.False(t, got)
			require.True(t, stringNext, "a string next that is not JSON is left to validateForwardMemo")
			return
		}
		require.Equal(t, decoderTokens(s), pfmTokens(s))
		want := refMemoWithinLimits(s, forwardMemo)
		require.Equal(t, want, got)
		require.Equal(t, want, walked)
		require.Equal(t, len(s) <= maxPFMForwardMemoLength && refMemoWithinLimits(s, true), pfmForwardMemoWithinLimits(s))
		require.Equal(t, firstNonSpace(memo) == '{' && refMemoWithinLimits(s, false), stringNext)
		checkScanObject(t, memo)
	})
}

// FuzzValidateDeterministicMsgs checks the filter on arbitrary ICS-20 packet data in a MsgRecvPacket, alone and
// nested in MsgExec and MsgSubmitProposal.
func FuzzValidateDeterministicMsgs(f *testing.F) {
	for _, memo := range seedMemos() {
		data, err := json.Marshal(transfertypes.FungibleTokenPacketData{Denom: "utia", Amount: "1", Sender: "s", Receiver: "r", Memo: memo})
		require.NoError(f, err)
		f.Add(data, true)
	}
	for _, data := range []string{`[1]`, `null`, `{"amount":"1","denom":"utia","receiver":"r","sender":"s","\u006demo":"x"}`, `{"amount":"1","Memo":"x"}`, `{"memo":"","memo":"x"}`, `{"forward":5}`} {
		f.Add([]byte(data), true)
	}
	addr := sdk.AccAddress("addr________________")
	validate := func(t *testing.T, data []byte, port string, wrap int) error {
		packet := channeltypes.NewPacket(data, 1, port, "channel-0", port, "channel-1", channeltypes.Packet{}.TimeoutHeight, 1)
		var msg sdk.Msg = channeltypes.NewMsgRecvPacket(packet, []byte("proof"), channeltypes.Packet{}.TimeoutHeight, addr.String())
		switch wrap {
		case 1:
			exec := authz.NewMsgExec(addr, []sdk.Msg{msg})
			msg = &exec
		case 2:
			proposal, err := govv1.NewMsgSubmitProposal([]sdk.Msg{msg}, nil, addr.String(), "", "title", "summary", false)
			require.NoError(t, err)
			msg = proposal
		}
		return validateDeterministicMsgs(msgsTx{msgs: []sdk.Msg{msg}})
	}
	f.Fuzz(func(t *testing.T, data []byte, transferPort bool) {
		before := bytes.Clone(data)
		if !transferPort {
			// Only the transfer port routes to the packet forward middleware.
			require.NoError(t, validate(t, data, "icahost", 0))
			return
		}
		err := validate(t, data, transfertypes.PortID, 0)
		require.Equal(t, before, data, "the input must not change")
		if err != nil {
			require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
		}
		// Bytes after the data in its backing array do not change the verdict.
		padded := append(bytes.Clone(data), `"}],\`...)[:len(data)]
		require.Equal(t, fmt.Sprint(err), fmt.Sprint(validate(t, padded, transfertypes.PortID, 0)))
		// Nesting the packet in MsgExec or MsgSubmitProposal does not change the verdict.
		require.Equal(t, fmt.Sprint(err), fmt.Sprint(validate(t, data, transfertypes.PortID, 1)))
		require.Equal(t, fmt.Sprint(err), fmt.Sprint(validate(t, data, transfertypes.PortID, 2)))
		if err != nil {
			return
		}
		var ftpd transfertypes.FungibleTokenPacketData
		if transfertypes.ModuleCdc.UnmarshalJSON(data, &ftpd) != nil {
			return
		}
		// The middleware decodes the packet data with ModuleCdc, so it sees a memo the filter accepts.
		require.NoError(t, checkForwardMemo(t, ftpd.Memo))
		// encoding/json reads the same memo from accepted packet data.
		var std transfertypes.FungibleTokenPacketData
		if json.Unmarshal(data, &std) == nil {
			require.Equal(t, ftpd.Memo, std.Memo)
		}
	})
}

// FuzzForwardMemoStructured checks generated forward-shaped memos as FuzzValidateForwardMemo and
// FuzzPFMMemoWithinLimits do, and that the filter accepts every memo the generator marks clean.
func FuzzForwardMemoStructured(f *testing.F) {
	f.Add([]byte{})
	for i := range 32 {
		f.Add(bytes.Repeat([]byte{byte(i)}, 64))
		f.Add([]byte{0, byte(i), 2, 6, byte(i), 1, 3, 2, 3, 2, 5, 2, 1, 4, 2, byte(i), 5, 5, 6})
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		g := &memoGen{in: in, clean: true}
		g.memo()
		memo := g.sb.String()
		err := checkForwardMemo(t, memo)
		if bz := []byte(memo); json.Valid(bz) {
			require.Equal(t, refMemoWithinLimits(memo, true), pfmMemoWithinLimits(bz, true), memo)
		}
		if g.clean {
			require.NoError(t, err, memo)
		}
	})
}

// checkForwardMemo checks that validateForwardMemo is deterministic and, if it accepts memo, checkPFMForwards.
func checkForwardMemo(t *testing.T, memo string) error {
	t.Helper()
	err := validateForwardMemo(memo)
	require.Equal(t, fmt.Sprint(err), fmt.Sprint(validateForwardMemo(memo)))
	if err == nil {
		checkPFMForwards(t, memo)
	}
	return err
}

// checkPFMForwards runs the memo handling of packet-forward-middleware v8.2.0 on an accepted memo. If the middleware
// forwards it, it must decode and validate the metadata, convert a numeric timeout exactly, and send as the next
// memo a JSON object within the limits that holds the same value as forward.next.
func checkPFMForwards(t *testing.T, memo string) {
	t.Helper()
	d := make(map[string]any)
	if json.Unmarshal([]byte(memo), &d) != nil || d["forward"] == nil {
		return // OnRecvPacket does not forward the packet.
	}
	var m pfmtypes.PacketMetadata
	require.NoError(t, json.Unmarshal([]byte(memo), &m), memo)
	require.NotNil(t, m.Forward, "OnRecvPacket would dereference a nil forward: %s", memo)
	require.NoError(t, m.Forward.Validate(), memo)

	// raw holds the forward fields as PacketMetadata matches them, without case.
	var raw struct {
		Forward struct{ Timeout, Next json.RawMessage }
	}
	require.NoError(t, json.Unmarshal([]byte(memo), &raw), memo)
	if v := raw.Forward.Timeout; len(v) > 0 && (v[0] == '-' || v[0] >= '0' && v[0] <= '9') {
		var timeout float64
		require.NoError(t, json.Unmarshal(v, &timeout), memo)
		require.Equal(t, timeout, float64(time.Duration(m.Forward.Timeout)), "inexact timeout conversion: %s", memo)
	}

	if m.Forward.Next == nil {
		return
	}
	next, err := json.Marshal(m.Forward.Next)
	require.NoError(t, err, "ForwardTransferPacket fails to encode next: %s", memo)
	require.Equal(t, byte('{'), firstNonSpace(next), memo)
	require.True(t, refMemoWithinLimits(string(next), false), "next memo %s from %s", next, memo)
	want := []byte(raw.Forward.Next)
	if want[0] == '"' {
		var s string
		require.NoError(t, json.Unmarshal(want, &s))
		want = []byte(s)
	}
	require.True(t, sameJSON(next, want), "next memo %s, forward.next %s", next, want)
}

// sameJSON reports whether the valid JSON texts a and b hold the same value as encoding/json decodes it.
// Texts with a number out of the float64 range compare equal if their compact, HTML-escaped forms are equal.
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil {
		return reflect.DeepEqual(x, y)
	}
	var ca, cb, ha, hb bytes.Buffer
	_, _ = json.Compact(&ca, a), json.Compact(&cb, b)
	json.HTMLEscape(&ha, ca.Bytes())
	json.HTMLEscape(&hb, cb.Bytes())
	return bytes.Equal(ha.Bytes(), hb.Bytes())
}

// refMemoWithinLimits is a slow reference for pfmWalkMemo on valid JSON, built on json.Decoder.
func refMemoWithinLimits(s string, forwardMemo bool) bool {
	type frame struct {
		object, fold, forward, wantKey bool
		keys                           []string
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var stack []*frame
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return len(stack) == 0
		}
		if err != nil {
			return false
		}
		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		if key, ok := tok.(string); ok && top != nil && top.object && top.wantKey {
			for _, k := range top.keys {
				if k == key || top.fold && strings.EqualFold(k, key) {
					return false
				}
			}
			top.keys = append(top.keys, key)
			top.wantKey = false
			continue
		}
		if tok == json.Delim('}') || tok == json.Delim(']') {
			stack = stack[:len(stack)-1]
			continue
		}
		if top != nil && top.object {
			top.wantKey = true
			if top.forward && strings.EqualFold(top.keys[len(top.keys)-1], "next") && !refNextWithinLimits(tok) {
				return false
			}
		}
		if delim, ok := tok.(json.Delim); ok {
			if len(stack) == maxPFMForwardMemoDepth {
				return false
			}
			f := &frame{object: delim == '{', wantKey: delim == '{'}
			if f.object && forwardMemo {
				f.forward = len(stack) == 1 && top.object && strings.EqualFold(top.keys[len(top.keys)-1], "forward")
				f.fold = len(stack) == 0 || f.forward
			}
			stack = append(stack, f)
		}
	}
}

// refNextWithinLimits reports whether tok starts an accepted forward.next: null, an object, a string that is
// not JSON, or a string holding a JSON object within the limits.
func refNextWithinLimits(tok json.Token) bool {
	switch v := tok.(type) {
	case nil:
		return true
	case json.Delim:
		return v == '{'
	case string:
		return !json.Valid([]byte(v)) || firstNonSpace([]byte(v)) == '{' && refMemoWithinLimits(v, false)
	}
	return false
}

// checkScanObject checks scanObject and matchKey against json.Decoder on valid JSON.
func checkScanObject(t *testing.T, bz []byte) {
	t.Helper()
	var keys, values [][]byte
	depth, isObject := scanObject(bz, func(key, value []byte) {
		keys = append(keys, key)
		values = append(values, value)
	})
	require.Equal(t, firstNonSpace(bz) == '{', isObject)
	if !isObject {
		return
	}
	maxDepth, level := 0, 0
	for _, tok := range decoderTokens(string(bz)) {
		switch tok {
		case json.Delim('{'), json.Delim('['):
			level++
			maxDepth = max(maxDepth, level)
		case json.Delim('}'), json.Delim(']'):
			level--
		}
	}
	require.Equal(t, maxDepth, depth)

	dec := json.NewDecoder(bytes.NewReader(bz))
	_, err := dec.Token()
	require.NoError(t, err)
	i := 0
	for ; dec.More(); i++ {
		tok, err := dec.Token()
		require.NoError(t, err)
		var value json.RawMessage
		require.NoError(t, dec.Decode(&value))
		require.Less(t, i, len(keys))
		var key string
		require.NoError(t, json.Unmarshal([]byte(`"`+string(keys[i])+`"`), &key))
		require.Equal(t, tok, key)
		require.Equal(t, bytes.TrimSpace(value), values[i])
		for _, name := range append(append([]string{"forward"}, ics20Fields...), forwardFields...) {
			exact, similar := matchKey(keys[i], name)
			stripped := strings.NewReplacer("_", "", "-", "").Replace(key)
			require.Equal(t, key == name, exact, "%q %q", key, name)
			require.Equal(t, strings.EqualFold(stripped, name), similar, "%q %q", key, name)
		}
	}
	require.Len(t, keys, i)
}

// decoderTokens returns the tokens of s from json.Decoder.Token with UseNumber.
func decoderTokens(s string) []json.Token {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var toks []json.Token
	for {
		tok, err := dec.Token()
		if err != nil {
			return toks
		}
		toks = append(toks, tok)
	}
}

// pfmTokens returns the tokens of s from pfmNextToken, as json.Decoder.Token returns them.
func pfmTokens(s string) []json.Token {
	var toks []json.Token
	for i := 0; ; {
		tok, end, ok := pfmNextToken(s, i)
		if !ok {
			return toks
		}
		switch {
		case tok.kind == '"':
			toks = append(toks, tok.text)
		case tok.kind == 'n':
			toks = append(toks, nil)
		case tok.kind == 'v' && (tok.text == "true" || tok.text == "false"):
			toks = append(toks, tok.text == "true")
		case tok.kind == 'v':
			toks = append(toks, json.Number(tok.text))
		default:
			toks = append(toks, json.Delim(tok.kind))
		}
		i = end
	}
}

func firstNonSpace(bz []byte) byte {
	if s := bytes.TrimLeft(bz, " \t\r\n"); len(s) > 0 {
		return s[0]
	}
	return 0
}

// memoGen writes forward-shaped JSON chosen by fuzz input. clean stays true while every choice is one the
// filter must accept; with no input left, every choice is the first, clean one.
type memoGen struct {
	in     []byte
	sb     strings.Builder
	depth  int
	spaces bool
	clean  bool
}

func (g *memoGen) pick(n int) int {
	if len(g.in) == 0 {
		return 0
	}
	b := g.in[0]
	g.in = g.in[1:]
	return int(b) % n
}

// choose writes one of the clean options, or one in eight times one of the dirty options.
func (g *memoGen) choose(clean []string, dirty ...string) {
	if len(clean) == 0 || len(dirty) > 0 && g.pick(8) == 7 {
		g.clean = false
		g.sb.WriteString(dirty[g.pick(len(dirty))])
		return
	}
	g.sb.WriteString(clean[g.pick(len(clean))])
}

func (g *memoGen) ws() {
	if g.spaces {
		g.sb.WriteString([]string{"", " ", "\n\t", "\r\n "}[g.pick(4)])
	}
}

func (g *memoGen) open(delim string) {
	g.depth++
	if g.depth > maxPFMForwardMemoDepth {
		g.clean = false
	}
	g.sb.WriteString(delim)
	g.ws()
}

func (g *memoGen) close(delim string) {
	g.ws()
	g.depth--
	g.sb.WriteString(delim)
}

func (g *memoGen) comma() {
	g.ws()
	g.sb.WriteString(",")
	g.ws()
}

// key writes the key name, escaped or not; a case or '_' variant is dirty.
func (g *memoGen) key(name string) {
	switch g.pick(16) {
	case 1, 4:
		name = fmt.Sprintf(`\u%04x`, name[0]) + name[1:]
	case 2:
		g.clean = false
		name = strings.ToUpper(name)
	case 3:
		g.clean = false
		name = name[:1] + "_" + name[1:]
	}
	g.sb.WriteString(`"` + name + `":`)
	g.ws()
}

// memo writes a forward memo, maybe padded to about the length limit.
func (g *memoGen) memo() {
	g.spaces = g.pick(2) == 1
	g.ws()
	g.open("{")
	seen := map[string]bool{}
	extra := func() {
		name := []string{"wasm", "a", "b", "callback", "memo", "next", "x", "A", "forward", "Forward"}[g.pick(10)]
		if seen[strings.ToLower(name)] {
			g.clean = false
		}
		seen[strings.ToLower(name)] = true
		g.key(name)
		g.value()
	}
	for range g.pick(2) {
		extra()
		g.comma()
	}
	if seen["forward"] {
		g.clean = false
	}
	seen["forward"] = true
	g.key("forward")
	g.forward()
	for range g.pick(3) {
		g.comma()
		extra()
	}
	if g.pick(4) == 1 {
		pad := transfertypes.MaximumMemoLength - 1 + g.pick(3) - g.sb.Len() - len(`,"pad":""}`)
		g.sb.WriteString(`,"pad":"` + strings.Repeat("p", max(pad, 0)) + `"`)
	}
	g.close("}")
	if g.sb.Len() > transfertypes.MaximumMemoLength {
		g.clean = false
	}
}

// forward writes the value of a forward key.
func (g *memoGen) forward() {
	switch g.pick(8) {
	case 6:
		g.sb.WriteString("null")
		return
	case 7:
		g.choose(nil, `5`, `[]`, `"abc"`, `true`)
		return
	}
	g.open("{")
	g.key("receiver")
	g.choose([]string{`"cosmos1receiver"`, `"r"`, `"x\ty\/z\"\\"`, `"\ud83d\ude00"`}, `""`, `5`, `null`, `"\udc00"`, "\"\xff\"")
	g.comma()
	g.key("port")
	g.choose([]string{`"transfer"`, `"tr"`, `"a.b_c+d#[x]<y>"`}, `""`, `"a/b"`, `"a"`, `true`)
	g.comma()
	g.key("channel")
	g.choose([]string{`"channel-0"`, `"channel-7"`, `"notachannel"`}, `""`, `"channel/1"`, `null`)
	seen := map[string]bool{}
	for range g.pick(5) {
		name := []string{"next", "timeout", "retries", "next", "memo", "receiver"}[g.pick(6)]
		if seen[name] || name == "receiver" {
			g.clean = false
		}
		seen[name] = true
		g.comma()
		g.key(name)
		switch name {
		case "timeout":
			g.choose([]string{`"10m"`, `"1h30m"`, `"1\u006d"`, `600000000000`, `6e11`, `0`, `-1`, `0.0`, `9223372036854774784`, `-9223372036854775808`},
				`1.5`, `1e19`, `1e400`, `9223372036854775807`, `9223372036854775808`, `"abc"`, `"9999999999h"`, `true`, `null`, `{}`)
		case "retries":
			g.choose([]string{`0`, `2`, `255`, `null`}, `300`, `-1`, `1.5`, `1e0`, `"1"`)
		case "next":
			g.next()
		default:
			g.value()
		}
	}
	g.close("}")
}

// next writes a forward.next value.
func (g *memoGen) next() {
	switch g.pick(6) {
	case 1:
		g.object()
	case 2:
		g.open("{")
		g.key("forward")
		g.forward()
		g.close("}")
	case 3:
		// A string next is the memo of the forwarded packet, so its depth starts again at 0.
		s := &memoGen{in: g.in, spaces: g.spaces, clean: true}
		if s.pick(2) == 0 {
			s.memo()
		} else {
			s.object()
		}
		g.in = s.in
		g.clean = g.clean && s.clean
		g.sb.WriteString(jsonString(s.sb.String()))
	case 4, 5:
		g.choose(nil, `"abc"`, `""`, `"[1,2]"`, `"null"`, `"5"`, `"\"{}\""`, `"{\"a\":"`, `5`, `[1,{"a":1}]`, `true`)
	default:
		g.sb.WriteString("null")
	}
}

// value writes any JSON value, sometimes nested to around the depth limit.
func (g *memoGen) value() {
	if g.depth > maxPFMForwardMemoDepth+2 {
		g.sb.WriteString("1")
		return
	}
	switch g.pick(8) {
	case 1, 2:
		g.choose([]string{`"s"`, `"\u00e9"`, `"\ud83d\ude00"`, `"\/\b\f\n\r\t"`, `"{[\"{"`, `"\u2028<>&"`, `"ſK"`},
			`"\ud800"`, `"\ude00\ud83d"`, "\"\xff\"", `"\ud800\u0041"`)
	case 3:
		g.choose([]string{`true`, `false`, `null`, `-0`, `1.5E-3`, `12345678901234567891`, `1e400`})
	case 4:
		g.open("[")
		for i := range g.pick(4) {
			if i > 0 {
				g.comma()
			}
			g.value()
		}
		g.close("]")
	case 5, 6:
		g.object()
	case 7:
		// Nest to depth 15, 16 or 17.
		var closers []string
		for range maxPFMForwardMemoDepth - 1 + g.pick(3) - g.depth {
			if g.pick(2) == 0 {
				g.open("[")
				closers = append(closers, "]")
			} else {
				g.open(`{"a":`)
				closers = append(closers, "}")
			}
		}
		g.sb.WriteString("1")
		for _, closer := range slices.Backward(closers) {
			g.close(closer)
		}
	default:
		g.sb.WriteString("0")
	}
}

// object writes an object with keys inside next, where only an exact repeat is dirty.
func (g *memoGen) object() {
	g.open("{")
	seen := map[string]bool{}
	for i := range g.pick(4) {
		if i > 0 {
			g.comma()
		}
		keys := [][2]string{{"a", "a"}, {`\u0061`, "a"}, {"A", "A"}, {"b", "b"}, {"next", "next"}, {"Next", "Next"}, {"forward", "forward"}, {"", ""}}
		k := keys[g.pick(len(keys))]
		if seen[k[1]] {
			g.clean = false
		}
		seen[k[1]] = true
		g.sb.WriteString(`"` + k[0] + `":`)
		g.ws()
		g.value()
	}
	g.close("}")
}
