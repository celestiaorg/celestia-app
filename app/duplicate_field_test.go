package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/encoding/protowire"
)

// appendBytesField appends a length-delimited field with the given number.
func appendBytesField(dst []byte, num protowire.Number, value []byte) []byte {
	dst = protowire.AppendTag(dst, num, protowire.BytesType)
	return protowire.AppendBytes(dst, value)
}

func TestHasDuplicateTxRawField(t *testing.T) {
	body := []byte("body")
	authInfo := []byte("authInfo")
	sig := []byte("sig")

	// A well-formed TxRaw: body_bytes(1), auth_info_bytes(2), signatures(3).
	honest := appendBytesField(nil, 1, body)
	honest = appendBytesField(honest, 2, authInfo)
	honest = appendBytesField(honest, 3, sig)

	// Same as honest but with a second signature (field 3). ADR-027 permits
	// multiple signatures.
	multiSig := appendBytesField(honest, 3, []byte("sig2"))

	// A duplicated body_bytes (field 1) is the ambiguous encoding.
	dupBody := appendBytesField(nil, 1, []byte("other"))
	dupBody = appendBytesField(dupBody, 1, body)
	dupBody = appendBytesField(dupBody, 2, authInfo)
	dupBody = appendBytesField(dupBody, 3, sig)

	// A duplicated auth_info_bytes (field 2).
	dupAuthInfo := appendBytesField(nil, 1, body)
	dupAuthInfo = appendBytesField(dupAuthInfo, 2, authInfo)
	dupAuthInfo = appendBytesField(dupAuthInfo, 2, []byte("other"))
	dupAuthInfo = appendBytesField(dupAuthInfo, 3, sig)

	testCases := []struct {
		name string
		tx   []byte
		want bool
	}{
		{"honest tx", honest, false},
		{"multiple signatures", multiSig, false},
		{"empty", []byte{}, false},
		{"garbage", []byte{0xff, 0xff, 0xff}, false},
		{"duplicate body_bytes", dupBody, true},
		{"duplicate auth_info_bytes", dupAuthInfo, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hasDuplicateTxRawField(tc.tx))
		})
	}
}
