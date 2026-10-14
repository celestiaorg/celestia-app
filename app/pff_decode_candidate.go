package app

import (
	"github.com/celestiaorg/celestia-app/v10/pkg/txutil"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/protobuf/encoding/protowire"
)

// pffDecodeCandidate recognizes ordinary single-PFF envelopes without copying
// their message payload. The SDK decoder remains authoritative.
func pffDecodeCandidate(raw []byte) bool {
	body, plain := txutil.PlainSDKBody(raw)
	if plain && body[0] == 0x0a {
		if anyBytes, n := protowire.ConsumeBytes(body[1:]); n == len(body)-1 && len(anyBytes) > 0 && anyBytes[0] == 0x0a {
			if typeURL, n := protowire.ConsumeString(anyBytes[1:]); n >= 0 {
				rest := anyBytes[1+n:]
				if len(rest) > 0 && rest[0] == 0x12 {
					if _, n := protowire.ConsumeBytes(rest[1:]); n == len(rest)-1 {
						return typeURL == fibretypes.MsgPayForFibreTypeURL
					}
				}
			}
		}
	}
	_, ok := fibretypes.ParsePayForFibreMsg(raw)
	return ok
}
