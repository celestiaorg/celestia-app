package app

import (
	"github.com/celestiaorg/celestia-app/v10/pkg/txutil"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/protobuf/encoding/protowire"
)

// pffDecodeCandidate recognizes ordinary single-PFF envelopes without copying
// their message payload. The SDK decoder remains authoritative.
func pffDecodeCandidate(raw []byte) bool {
	if typeURL, ok := singleMessageTypeURL(raw); ok {
		return typeURL == fibretypes.MsgPayForFibreTypeURL
	}
	return fibretypes.IsPayForFibreTx(raw)
}

// singleMessageTypeURL returns the type URL of a plain tx whose body is exactly
// one message, an Any of a type URL then a value, and false for any other shape.
func singleMessageTypeURL(raw []byte) (string, bool) {
	body, plain := txutil.PlainSDKBody(raw)
	if !plain || body[0] != 0x0a {
		return "", false
	}
	anyBytes, n := protowire.ConsumeBytes(body[1:])
	if n != len(body)-1 || len(anyBytes) == 0 || anyBytes[0] != 0x0a {
		return "", false
	}
	typeURL, n := protowire.ConsumeString(anyBytes[1:])
	if n < 0 {
		return "", false
	}
	rest := anyBytes[1+n:]
	if len(rest) == 0 || rest[0] != 0x12 {
		return "", false
	}
	if _, n := protowire.ConsumeBytes(rest[1:]); n != len(rest)-1 {
		return "", false
	}
	return typeURL, true
}
