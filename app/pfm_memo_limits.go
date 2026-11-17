package app

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

// maxPFMForwardMemoLength is the longest forward memo accepted, in bytes: the ibc-go MsgTransfer memo limit.
const maxPFMForwardMemoLength = transfertypes.MaximumMemoLength

// maxPFMForwardMemoDepth bounds forward memo nesting, and so the work to encode forward.next; 16 levels allow 8 hops.
const maxPFMForwardMemoDepth = 16

// pfmForwardMemoWithinLimits reports whether memo, valid JSON, is within the length, depth, repeated-key and
// forward.next limits. They keep the middleware's work per memo and per retry linear in the memo length.
func pfmForwardMemoWithinLimits(memo string) bool {
	return len(memo) <= maxPFMForwardMemoLength && pfmWalkMemo(memo, true)
}

// pfmMemoFrame is an object or array that pfmMemoWithinLimits is inside.
type pfmMemoFrame struct {
	object bool
	// keys holds the keys of an object once it has two; before that, key holds the only one.
	keys map[string]struct{}
	// hasKey is true once the object has a key.
	hasKey bool
	// fold is true for an object that encoding/json decodes into a struct, so its keys match without case.
	fold bool
	// forward is true for the object that holds the forward metadata.
	forward bool
	// key is the last key of an object, folded if fold is true.
	key string
	// expectKey is true when the next string in an object is a key.
	expectKey bool
}

// pfmMemoWithinLimits reports whether bz is one valid JSON value within the depth and repeated-key limits.
// For a forward memo, struct field keys compare without case and forward.next must have an accepted form.
func pfmMemoWithinLimits(bz []byte, forwardMemo bool) bool {
	return json.Valid(bz) && pfmWalkMemo(string(bz), forwardMemo)
}

// pfmWalkMemo is pfmMemoWithinLimits for s that is known to be valid JSON.
// It reads tokens with pfmNextToken, which is faster than json.Decoder.Token and does not allocate per token.
func pfmWalkMemo(s string, forwardMemo bool) bool {
	w := pfmMemoWalker{forwardMemo: forwardMemo}
	for i := 0; ; {
		tok, end, ok := pfmNextToken(s, i)
		if !ok {
			// Empty input or an unclosed object or array is not within limits.
			return i > 0 && len(w.stack) == 0
		}
		if !w.next(tok) {
			return false
		}
		i = end
	}
}

// pfmToken is a JSON token, as json.Decoder.Token returns it.
type pfmToken struct {
	// kind is the delimiter, '"' for a string, 'n' for null, or 'v' for a number, true or false.
	kind byte
	// text is the decoded string, or the text of a number, true or false.
	text string
}

// pfmNextToken returns the token at or after s[i] in the valid JSON text s and the index after it, or ok false
// at the end of s. It does not panic on other input.
func pfmNextToken(s string, i int) (tok pfmToken, end int, ok bool) {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n' || s[i] == ',' || s[i] == ':') {
		i++
	}
	if i >= len(s) {
		return pfmToken{}, i, false
	}
	switch c := s[i]; c {
	case '{', '}', '[', ']':
		return pfmToken{kind: c}, i + 1, true
	case '"':
		end = skipJSONString(s, i)
		raw := s[i+1 : max(end-1, i+1)]
		if strings.IndexByte(raw, '\\') < 0 && utf8.ValidString(raw) {
			return pfmToken{kind: '"', text: raw}, end, true
		}
		return pfmToken{kind: '"', text: unquoteJSONString(raw)}, end, true
	case 'n':
		return pfmToken{kind: 'n'}, i + 4, true
	}
	// A number, true or false.
	end = i + 1
	for end < len(s) && strings.IndexByte(" \t\r\n,:]}", s[end]) < 0 {
		end++
	}
	return pfmToken{kind: 'v', text: s[i:end]}, end, true
}

// unquoteJSONString decodes a raw JSON string as encoding/json does, with U+FFFD for invalid UTF-8 and lone
// surrogates.
func unquoteJSONString(raw string) string {
	var sb strings.Builder
	sb.Grow(len(raw))
	bz := []byte(raw)
	for i := 0; i < len(bz); {
		r, n := decodeStringRune(bz, i)
		sb.WriteRune(r)
		i += n
	}
	return sb.String()
}

// skipJSONString returns the index after the JSON string that starts at s[i].
func skipJSONString(s string, i int) int {
	for i++; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(s)
}

// pfmMemoWalker holds the objects and arrays that pfmMemoWithinLimits is inside.
type pfmMemoWalker struct {
	forwardMemo bool
	stack       []pfmMemoFrame
}

// next takes the next token and reports whether the memo is still within limits.
func (w *pfmMemoWalker) next(tok pfmToken) bool {
	var top *pfmMemoFrame
	if n := len(w.stack); n > 0 {
		top = &w.stack[n-1]
	}
	if tok.kind == '"' && top != nil && top.object && top.expectKey {
		return top.addKey(tok.text)
	}
	if tok.kind == '}' || tok.kind == ']' {
		if top == nil {
			// A closing delimiter with nothing open: not valid JSON.
			return false
		}
		w.stack = w.stack[:len(w.stack)-1]
		return true
	}
	// tok starts a value. In an object, a key follows the value.
	if top != nil && top.object {
		top.expectKey = true
	}
	if top != nil && top.forward && top.key == "NEXT" && !pfmForwardNextWithinLimits(tok) {
		return false
	}
	if tok.kind == '{' || tok.kind == '[' {
		return w.open(tok.kind == '{', top)
	}
	return true
}

// pfmForwardNextWithinLimits reports whether tok, the first token of forward.next, starts an accepted form:
// null, an object, or a string that pfmStringNextWithinLimits accepts.
func pfmForwardNextWithinLimits(tok pfmToken) bool {
	switch tok.kind {
	case 'n', '{':
		return true
	case '"':
		return pfmStringNextWithinLimits(tok.text)
	default:
		return false
	}
}

// open enters an object or array whose parent is top and reports whether the depth is within the limit.
func (w *pfmMemoWalker) open(object bool, top *pfmMemoFrame) bool {
	if len(w.stack) == maxPFMForwardMemoDepth {
		return false
	}
	f := pfmMemoFrame{object: object, expectKey: true}
	if object {
		f.forward = w.forwardMemo && len(w.stack) == 1 && top.key == "FORWARD"
		f.fold = w.forwardMemo && (len(w.stack) == 0 || f.forward)
	}
	w.stack = append(w.stack, f)
	return true
}

// addKey records key in the object f and reports whether it is new.
func (f *pfmMemoFrame) addKey(key string) bool {
	if f.fold {
		key = foldJSONKey(key)
	}
	switch {
	case !f.hasKey:
		// The first key needs no map, so an object with one key costs none.
		f.hasKey = true
	case f.keys == nil:
		if key == f.key {
			return false
		}
		f.keys = map[string]struct{}{f.key: {}, key: {}}
	default:
		if _, repeated := f.keys[key]; repeated {
			return false
		}
		f.keys[key] = struct{}{}
	}
	f.key = key
	f.expectKey = false
	return true
}

// pfmStringNextWithinLimits reports whether a string forward.next is accepted. If it is JSON, it becomes the
// next memo and must pass pfmNextMemoWithinLimits.
func pfmStringNextWithinLimits(s string) bool {
	if !json.Valid([]byte(s)) {
		// The middleware cannot encode it and returns an error acknowledgement.
		return true
	}
	return pfmNextMemoWithinLimits(s)
}

// pfmNextMemoWithinLimits reports whether memo, valid JSON, is an object within the depth and repeated-key limits.
func pfmNextMemoWithinLimits(memo string) bool {
	trimmed := strings.TrimLeft(memo, " \t\r\n")
	return trimmed != "" && trimmed[0] == '{' && pfmWalkMemo(memo, false)
}

// foldJSONKey folds key as encoding/json does to match struct fields: two keys match if their folded forms are equal.
func foldJSONKey(key string) string {
	out := make([]byte, 0, len(key))
	for _, r := range key {
		if r < utf8.RuneSelf {
			if 'a' <= r && r <= 'z' {
				r -= 'a' - 'A'
			}
			out = append(out, byte(r))
			continue
		}
		// Use the smallest rune in the fold set of r.
		for {
			r2 := unicode.SimpleFold(r)
			if r2 <= r {
				r = r2
				break
			}
			r = r2
		}
		out = utf8.AppendRune(out, r)
	}
	return string(out)
}
