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

// maxPFMForwardMemoLength is the longest forward memo accepted, in bytes. It
// is the limit that ibc-go applies to the memo of a MsgTransfer, so a packet
// sent by an ibc-go chain is never longer.
const maxPFMForwardMemoLength = transfertypes.MaximumMemoLength

// maxPFMForwardMemoDepth is the deepest JSON nesting of objects and arrays
// accepted in a forward memo, and in the memo held by a string forward.next.
// packet-forward-middleware encodes forward.next again to make the memo of
// the forwarded packet, and decodes and encodes that memo again each time
// the forwarded packet times out and is retried. Each encoding re-reads the
// bytes at every nesting level, so the work grows with the depth times the
// length. A memo nests two levels per hop ({"forward":{"next":...}}), so 32
// levels allow 15 hops, or a few hops followed by a nested memo for the last
// chain. Real routes use far fewer.
const maxPFMForwardMemoDepth = 32

// pfmForwardMemoWithinLimits reports whether a forward memo is within the
// limits that bound the work packet-forward-middleware does for it:
//   - the memo is at most maxPFMForwardMemoLength bytes,
//   - the memo nests at most maxPFMForwardMemoDepth levels,
//   - no object in the memo repeats a key. encoding/json decodes each
//     repeated "forward" or "next" key again into the same value, and the
//     ordered map that holds next handles repeated keys in quadratic time.
//     The keys of the memo and of the forward object compare the way
//     encoding/json matches struct fields (without case); other keys compare
//     exactly, as the ordered map keeps them.
//   - a string forward.next, which is the memo of the forwarded packet, is
//     within the same depth and repeated-key limits, and does not hold a
//     JSON string (which a retry would unwrap into another memo).
//
// With these limits, the work for one packet and for each retry is linear
// in maxPFMForwardMemoLength with a factor of maxPFMForwardMemoDepth. The
// memo of the forwarded packet can be longer than the original memo, up to
// six times, because json.Marshal escapes '<', '>' and '&'. The check reads
// memo in one pass and does not panic on any input.
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
// maxPFMForwardMemoDepth levels and has no repeated object key. When
// forwardMemo is true, bz is a forward memo: the keys of the memo and of its
// forward object compare without case, and a string forward.next is checked
// with pfmStringNextWithinLimits.
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
	switch t := tok.(type) {
	case json.Delim:
		return w.open(t == '{', top)
	case string:
		return top == nil || !top.forward || top.key != "NEXT" || pfmStringNextWithinLimits(t)
	}
	return true
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

// pfmStringNextWithinLimits reports whether the memo s held by a string
// forward.next is within limits. packet-forward-middleware uses s as the
// memo of the forwarded packet, and decodes it again if that packet times
// out and is retried.
func pfmStringNextWithinLimits(s string) bool {
	bz := []byte(s)
	if !json.Valid(bz) {
		// packet-forward-middleware fails to encode it and returns an error
		// acknowledgement.
		return true
	}
	if bytes.TrimLeft(bz, " \t\r\n")[0] == '"' {
		return false
	}
	return pfmMemoWithinLimits(bz, false)
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
