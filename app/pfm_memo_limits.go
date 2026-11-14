package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode"
	"unicode/utf8"

	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

// maxPFMForwardMemoLength is the longest forward memo accepted, in bytes.
// ibc-go applies the same limit to the memo of a MsgTransfer.
const maxPFMForwardMemoLength = transfertypes.MaximumMemoLength

// maxPFMForwardMemoDepth is the deepest JSON nesting accepted in a forward
// memo. The work to encode forward.next grows with the depth times the
// length. Each hop adds two levels, so 16 levels allow 8 hops.
const maxPFMForwardMemoDepth = 16

// pfmForwardMemoWithinLimits reports whether a forward memo is short, not
// deeply nested, has no repeated key, and has a forward.next of an accepted
// form. These limits make the work of packet-forward-middleware for the
// memo, and for each retry, linear in its length.
func pfmForwardMemoWithinLimits(memo string) bool {
	return len(memo) <= maxPFMForwardMemoLength && pfmMemoWithinLimits([]byte(memo), true)
}

// pfmMemoFrame is an object or array that pfmMemoWithinLimits is inside.
type pfmMemoFrame struct {
	object bool
	// keys holds the keys seen so far in an object.
	keys map[string]struct{}
	// fold is true for an object that encoding/json decodes into a struct,
	// whose keys match without case.
	fold bool
	// forward is true for the object that holds the forward metadata.
	forward bool
	// key is the last key of an object, folded if fold is true.
	key string
	// expectKey is true when the next string in an object is a key.
	expectKey bool
}

// pfmMemoWithinLimits reports whether the JSON value bz nests at most
// maxPFMForwardMemoDepth levels and repeats no object key. For a forward
// memo, the keys that encoding/json matches to struct fields compare without
// case, and forward.next must be an accepted form.
func pfmMemoWithinLimits(bz []byte, forwardMemo bool) bool {
	dec := json.NewDecoder(bytes.NewReader(bz))
	dec.UseNumber()
	w := pfmMemoWalker{forwardMemo: forwardMemo}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return true
		}
		if err != nil || !w.next(tok) {
			return false
		}
	}
}

// pfmMemoWalker holds the objects and arrays that pfmMemoWithinLimits is
// inside.
type pfmMemoWalker struct {
	forwardMemo bool
	stack       []pfmMemoFrame
}

// next takes the next token and reports whether the memo is still within
// limits.
func (w *pfmMemoWalker) next(tok json.Token) bool {
	var top *pfmMemoFrame
	if n := len(w.stack); n > 0 {
		top = &w.stack[n-1]
	}
	if key, ok := tok.(string); ok && top != nil && top.object && top.expectKey {
		return top.addKey(key)
	}
	if delim, ok := tok.(json.Delim); ok && (delim == '}' || delim == ']') {
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
	if delim, ok := tok.(json.Delim); ok {
		return w.open(delim == '{', top)
	}
	return true
}

// pfmForwardNextWithinLimits reports whether tok, the first token of
// forward.next, starts an accepted form: null, an object, or a string
// (pfmStringNextWithinLimits).
func pfmForwardNextWithinLimits(tok json.Token) bool {
	switch t := tok.(type) {
	case nil:
		return true
	case json.Delim:
		return t == '{'
	case string:
		return pfmStringNextWithinLimits(t)
	default:
		return false
	}
}

// open enters an object or an array whose parent is top, and reports
// whether the depth is within the limit.
func (w *pfmMemoWalker) open(object bool, top *pfmMemoFrame) bool {
	if len(w.stack) == maxPFMForwardMemoDepth {
		return false
	}
	f := pfmMemoFrame{object: object, expectKey: true}
	if object {
		f.keys = make(map[string]struct{})
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
	if _, repeated := f.keys[key]; repeated {
		return false
	}
	f.keys[key] = struct{}{}
	f.key = key
	f.expectKey = false
	return true
}

// pfmStringNextWithinLimits reports whether a string forward.next is
// accepted. It becomes the memo of the forwarded packet, so if it is JSON it
// must pass pfmNextMemoWithinLimits.
func pfmStringNextWithinLimits(s string) bool {
	if !json.Valid([]byte(s)) {
		// packet-forward-middleware fails to encode it and returns an error
		// acknowledgement.
		return true
	}
	return pfmNextMemoWithinLimits(s)
}

// maxPFMRetryMemoLength is the longest forwarded memo that may be retried.
// json.Marshal can make forward.next up to six times longer.
const maxPFMRetryMemoLength = 6 * maxPFMForwardMemoLength

// pfmRetryMemoWithinLimits reports whether a forwarded packet with this memo
// may be retried after a timeout. Every memo forwarded under these limits
// passes; one forwarded before them may not.
func pfmRetryMemoWithinLimits(memo string) bool {
	if memo == "" {
		return true
	}
	return len(memo) <= maxPFMRetryMemoLength && json.Valid([]byte(memo)) && pfmNextMemoWithinLimits(memo)
}

// pfmNextMemoWithinLimits reports whether memo, valid JSON, is an object
// within the depth and repeated-key limits.
func pfmNextMemoWithinLimits(memo string) bool {
	bz := []byte(memo)
	return bytes.TrimLeft(bz, " \t\r\n")[0] == '{' && pfmMemoWithinLimits(bz, false)
}

// foldJSONKey folds key the way encoding/json does to match an object key
// to a struct field name: two keys match if and only if their folded forms
// are equal.
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
