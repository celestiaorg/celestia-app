package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	pfmtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
)

// validateDeterministicMsgs returns an error if tx carries a message in a form
// this node does not admit (see checkDeterministicMsg), either at the top level or one level down in a MsgExec or MsgSubmitProposal.
// Deeper nesting is not inspected because the ante handler rejects it.
//
// This is a local mempool and proposal policy, not a validity rule:
// ProcessProposal and FinalizeBlock still accept such a tx, so blocks built by
// nodes without this check stay valid.
func validateDeterministicMsgs(tx sdk.Tx) error {
	for _, msg := range tx.GetMsgs() {
		if err := checkDeterministicMsg(msg); err != nil {
			return err
		}
		var (
			nested []sdk.Msg
			err    error
		)
		switch m := msg.(type) {
		case *authz.MsgExec:
			nested, err = m.GetMessages()
		case *govv1.MsgSubmitProposal:
			nested, err = m.GetMsgs()
		}
		if err != nil {
			// Undecodable inner messages are rejected by the ante handler.
			continue
		}
		for _, nestedMsg := range nested {
			if err := checkDeterministicMsg(nestedMsg); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkDeterministicMsg(msg sdk.Msg) error {
	switch m := msg.(type) {
	case *channeltypes.MsgRecvPacket:
		p := m.Packet
		// Only the transfer port routes to the packet forward middleware.
		if p.DestinationPort != transfertypes.PortID {
			return nil
		}
		if err := validateTransferPacketData(p.Data); err != nil {
			return sdkerrors.ErrInvalidRequest.Wrapf("unsupported packet-forward memo in packet %d from %q/%q to %q/%q: %s",
				p.Sequence, truncate(p.SourcePort), truncate(p.SourceChannel), truncate(p.DestinationPort), truncate(p.DestinationChannel), err)
		}
	case *govv1.MsgUpdateParams:
		if p := m.Params.MaxDepositPeriod; p != nil && *p <= 0 {
			return sdkerrors.ErrInvalidRequest.Wrap("gov max_deposit_period must be positive")
		}
	}
	return nil
}

// maxTransferPacketDataSize is the largest ICS-20 packet data inspected before
// the ante handler. It fits an ibc-go sender's largest memo and receiver, a
// 1023-byte sender, a 78-digit amount and a 16 KiB denom trace, each with every
// byte JSON-escaped to six bytes, plus 1 KiB for the field names.
const maxTransferPacketDataSize = 6*(transfertypes.MaximumMemoLength+transfertypes.MaximumReceiverLength+1023+78+16<<10) + 1<<10

// ics20Fields are the JSON keys of ICS-20 packet data.
var ics20Fields = []string{"amount", "denom", "memo", "receiver", "sender"}

// validateTransferPacketData returns an error if data is too large, holds a
// key other than the ICS-20 fields or a field twice, or carries a forward memo
// that validateForwardMemo rejects. Packet data the middleware cannot decode is
// passed on unchanged, so it is allowed.
func validateTransferPacketData(data []byte) error {
	if len(data) > maxTransferPacketDataSize {
		return fmt.Errorf("packet data is longer than %d bytes", maxTransferPacketDataSize)
	}
	seen := make(map[string]bool, len(ics20Fields))
	var keyErr error
	scanObject(data, func(key, _ []byte) {
		for _, name := range ics20Fields {
			if exact, _ := matchKey(key, name); exact {
				if seen[name] {
					keyErr = fmt.Errorf("packet data holds %q twice", name)
				}
				seen[name] = true
				return
			}
		}
		keyErr = fmt.Errorf("packet data holds a key other than the ICS-20 fields")
	})
	if keyErr != nil {
		return keyErr
	}
	var ftpd transfertypes.FungibleTokenPacketData
	if err := transfertypes.ModuleCdc.UnmarshalJSON(data, &ftpd); err != nil {
		return nil
	}
	return validateForwardMemo(ftpd.Memo)
}

// truncate shortens s for an error message.
func truncate(s string) string {
	const maxLen = 64
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// maxForwardMemoDepth is the deepest JSON nesting accepted in a forward memo.
// Each hop adds two levels (forward and next), so this allows about 30 hops.
const maxForwardMemoDepth = 64

var forwardFields = []string{"receiver", "port", "channel", "timeout", "retries", "next"}

// forwardMetadata mirrors pfmtypes.ForwardMetadata but keeps next raw, so that
// decoding it costs time linear in its size.
type forwardMetadata struct {
	Receiver string            `json:"receiver,omitempty"`
	Port     string            `json:"port,omitempty"`
	Channel  string            `json:"channel,omitempty"`
	Timeout  pfmtypes.Duration `json:"timeout,omitempty"`
	Retries  *uint8            `json:"retries,omitempty"`
	Next     *json.RawMessage  `json:"next,omitempty"`
}

// validateForwardMemo returns an error if the packet-forward middleware would
// treat memo as a forward request and memo is not in the supported form. Its
// cost is linear in the memo length.
func validateForwardMemo(memo string) error {
	bz := []byte(memo)
	var (
		fwd      []byte
		forwards int
		variant  bool
	)
	depth, isObject := scanObject(bz, func(key, value []byte) {
		exact, similar := matchKey(key, "forward")
		switch {
		case exact:
			forwards++
			fwd = value
		case similar:
			variant = true
		}
	})
	if !isObject || forwards == 0 || (forwards == 1 && bytes.Equal(fwd, []byte("null"))) {
		// The middleware forwards only a memo with a non-null top-level "forward" key.
		return nil
	}
	// JSON decoders differ in how deep a valid document may nest.
	if depth > maxForwardMemoDepth {
		return fmt.Errorf("memo nests deeper than %d levels", maxForwardMemoDepth)
	}
	if !json.Valid(bz) {
		// The middleware passes memos that are not JSON on unchanged.
		return nil
	}
	if forwards > 1 {
		return fmt.Errorf(`duplicate key "forward"`)
	}
	if variant {
		return fmt.Errorf(`a key other than "forward" must not match "forward" ignoring case`)
	}
	if len(memo) > transfertypes.MaximumMemoLength {
		return fmt.Errorf("memo is longer than %d bytes", transfertypes.MaximumMemoLength)
	}
	if !utf8.Valid(bz) || hasLoneSurrogate(bz) {
		return fmt.Errorf("memo must be valid UTF-8 without lone surrogate escapes")
	}

	fields := make(map[string][]byte, len(forwardFields))
	var fieldErr error
	if _, isObject := scanObject(fwd, func(key, value []byte) {
		for _, name := range forwardFields {
			exact, similar := matchKey(key, name)
			switch {
			case exact && fields[name] != nil:
				fieldErr = fmt.Errorf("duplicate key %q", name)
			case exact:
				fields[name] = value
			case similar:
				fieldErr = fmt.Errorf("a key other than %q must not match %q ignoring case", name, name)
			}
		}
	}); !isObject {
		return fmt.Errorf("forward must be a JSON object")
	}
	if fieldErr != nil {
		return fieldErr
	}
	for _, name := range []string{"receiver", "port", "channel"} {
		if v := fields[name]; v != nil && v[0] != '"' {
			return fmt.Errorf("forward.%s must be a string", name)
		}
	}
	timeout := fields["timeout"]
	numericTimeout := timeout != nil && (timeout[0] == '-' || (timeout[0] >= '0' && timeout[0] <= '9'))
	if timeout != nil && !numericTimeout && timeout[0] != '"' {
		return fmt.Errorf("forward.timeout must be a string or a number")
	}
	if v := fields["retries"]; v != nil && !isDigits(v) && !bytes.Equal(v, []byte("null")) {
		return fmt.Errorf("forward.retries must be a non-negative integer")
	}

	// Decode and validate as the middleware does.
	var metadata struct {
		Forward *forwardMetadata `json:"forward"`
	}
	if err := json.Unmarshal(bz, &metadata); err != nil {
		return fmt.Errorf("invalid forward metadata")
	}
	f := metadata.Forward
	if f == nil {
		return fmt.Errorf("missing forward metadata")
	}
	if err := (&pfmtypes.ForwardMetadata{Receiver: f.Receiver, Port: f.Port, Channel: f.Channel}).Validate(); err != nil {
		return fmt.Errorf("invalid forward metadata")
	}
	if f.Next != nil && (*f.Next)[0] == '"' {
		// The middleware re-encodes the content of a string next as the next
		// memo, which fails unless that content is JSON.
		var next string
		if err := json.Unmarshal(*f.Next, &next); err != nil {
			return fmt.Errorf("invalid forward.next")
		}
		nextBz := []byte(next)
		start := skipSpace(nextBz, 0)
		if _, depth := skipValue(nextBz, start); depth > maxForwardMemoDepth {
			return fmt.Errorf("forward.next nests deeper than %d levels", maxForwardMemoDepth)
		}
		if !json.Valid(nextBz) || hasLoneSurrogate(nextBz) {
			return fmt.Errorf("forward.next must be JSON")
		}
	}

	// Accept a numeric timeout only as an integer in the int64 range.
	if numericTimeout {
		var v float64
		if err := json.Unmarshal(timeout, &v); err != nil {
			return fmt.Errorf("invalid forward.timeout")
		}
		if v < -0x1p63 || v >= 0x1p63 || v != math.Trunc(v) {
			return fmt.Errorf("forward.timeout must be an integer in the int64 range")
		}
	}
	return nil
}

// scanObject calls fn with the raw key and raw value of each member of the
// JSON object in bz, and returns the nesting depth of bz. isObject is false if
// bz does not start with '{'. It tolerates malformed JSON and reads each byte
// a constant number of times.
func scanObject(bz []byte, fn func(key, value []byte)) (depth int, isObject bool) {
	i := skipSpace(bz, 0)
	if i >= len(bz) || bz[i] != '{' {
		return 0, false
	}
	depth = 1
	for i++; ; i++ {
		i = skipSpace(bz, i)
		if i >= len(bz) || bz[i] != '"' {
			return depth, true
		}
		keyEnd := skipString(bz, i)
		key := bz[i+1 : max(keyEnd-1, i+1)]
		i = skipSpace(bz, keyEnd)
		if i >= len(bz) || bz[i] != ':' {
			return depth, true
		}
		valueStart := skipSpace(bz, i+1)
		valueEnd, valueDepth := skipValue(bz, valueStart)
		depth = max(depth, 1+valueDepth)
		if valueStart < valueEnd {
			fn(key, bytes.TrimRight(bz[valueStart:valueEnd], " \t\r\n"))
		}
		i = skipSpace(bz, valueEnd)
		if i >= len(bz) || bz[i] != ',' {
			return depth, true
		}
	}
}

// skipValue returns the index after the JSON value that starts at bz[i], and
// the nesting depth of that value.
func skipValue(bz []byte, i int) (end, depth int) {
	level := 0
	for ; i < len(bz); i++ {
		switch bz[i] {
		case '"':
			i = skipString(bz, i) - 1
			if level == 0 {
				return i + 1, depth
			}
		case '{', '[':
			level++
			depth = max(depth, level)
		case '}', ']':
			if level == 0 {
				return i, depth
			}
			level--
			if level == 0 {
				return i + 1, depth
			}
		case ',':
			if level == 0 {
				return i, depth
			}
		}
	}
	return len(bz), depth
}

// skipString returns the index after the JSON string that starts at bz[i].
func skipString(bz []byte, i int) int {
	for i++; i < len(bz); i++ {
		switch bz[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(bz)
}

func skipSpace(bz []byte, i int) int {
	for i < len(bz) && (bz[i] == ' ' || bz[i] == '\t' || bz[i] == '\r' || bz[i] == '\n') {
		i++
	}
	return i
}

// matchKey reports whether the raw JSON string key decodes to name, and
// whether it matches name ignoring case, '_' and '-', as some JSON decoders do.
// name must be lower-case ASCII.
func matchKey(key []byte, name string) (exact, similar bool) {
	exact, similar = true, true
	exactLen, similarLen := 0, 0
	for i := 0; i < len(key) && (exact || similar); {
		r, n := decodeStringRune(key, i)
		i += n
		if exact {
			exact = exactLen < len(name) && r == rune(name[exactLen])
			exactLen++
		}
		if similar && r != '_' && r != '-' {
			similar = similarLen < len(name) && equalFoldRune(r, rune(name[similarLen]))
			similarLen++
		}
	}
	return exact && exactLen == len(name), similar && similarLen == len(name)
}

// equalFoldRune reports whether r equals t under Unicode simple case folding.
func equalFoldRune(r, t rune) bool {
	for f := t; ; {
		if f == r {
			return true
		}
		if f = unicode.SimpleFold(f); f == t {
			return false
		}
	}
}

// decodeStringRune decodes the rune at s[i] in the raw content of a JSON
// string, and returns it with the number of bytes it takes.
func decodeStringRune(s []byte, i int) (rune, int) {
	if s[i] != '\\' {
		return utf8.DecodeRune(s[i:])
	}
	if i+1 >= len(s) {
		return utf8.RuneError, 1
	}
	switch s[i+1] {
	case 'u':
		r := hexRune(s, i+2)
		if !utf16.IsSurrogate(r) {
			return r, 6
		}
		if i+7 < len(s) && s[i+6] == '\\' && s[i+7] == 'u' {
			if pair := utf16.DecodeRune(r, hexRune(s, i+8)); pair != utf8.RuneError {
				return pair, 12
			}
		}
		return utf8.RuneError, 6
	case 'b':
		return '\b', 2
	case 'f':
		return '\f', 2
	case 'n':
		return '\n', 2
	case 'r':
		return '\r', 2
	case 't':
		return '\t', 2
	}
	return rune(s[i+1]), 2
}

// hexRune returns the rune spelled by four hex digits at s[i], or
// utf8.RuneError if there are none.
func hexRune(s []byte, i int) rune {
	if i+4 > len(s) {
		return utf8.RuneError
	}
	var r rune
	for _, c := range s[i : i+4] {
		switch {
		case c >= '0' && c <= '9':
			c -= '0'
		case c >= 'a' && c <= 'f':
			c = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			c = c - 'A' + 10
		default:
			return utf8.RuneError
		}
		r = r<<4 | rune(c)
	}
	return r
}

// hasLoneSurrogate reports whether a string in the JSON text bz holds a \u
// escape of a UTF-16 surrogate that is not part of a valid pair.
func hasLoneSurrogate(bz []byte) bool {
	for i := 0; i < len(bz); i++ {
		if bz[i] != '"' {
			continue
		}
		end := skipString(bz, i)
		s := bz[i+1 : max(end-1, i+1)]
		for j := 0; j < len(s); j++ {
			if s[j] != '\\' {
				continue
			}
			if j+1 < len(s) && s[j+1] == 'u' && utf16.IsSurrogate(hexRune(s, j+2)) {
				if r, _ := decodeStringRune(s, j); r == utf8.RuneError {
					return true
				}
				j += 11 // Skip the rest of the pair.
				continue
			}
			j++
		}
		i = end - 1
	}
	return false
}

func isDigits(v []byte) bool {
	if len(v) == 0 {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
