package txutil

import (
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	"google.golang.org/protobuf/encoding/protowire"
)

// PlainSDKBody recognizes a bytes-only TxRaw envelope without wrapper markers.
// It does not validate the body; callers must still use the SDK decoder.
func PlainSDKBody(raw []byte) ([]byte, bool) {
	var body []byte
	for len(raw) > 0 {
		num, typ, n := protowire.ConsumeTag(raw)
		if n < 0 || typ != protowire.BytesType || num < 1 || num > 3 {
			return nil, false
		}
		value, size := protowire.ConsumeBytes(raw[n:])
		if size < 0 {
			return nil, false
		}
		if num == 1 {
			body = value
		}
		if num == 3 && (string(value) == squaretx.ProtoBlobTxTypeID || string(value) == squaretx.ProtoIndexWrapperTypeID) {
			return nil, false
		}
		raw = raw[n+size:]
	}
	return body, len(body) > 0
}
