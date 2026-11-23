package fibre

import (
	"bytes"
	"testing"
)

func TestPrefixUpperBound(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix []byte
		want   []byte
	}{
		{"regular", []byte("/shard/abc"), []byte("/shard/abd")},
		{"carry", []byte{0x01, 0xff}, []byte{0x02}},
		{"multiple carries", []byte{0x01, 0xff, 0xff}, []byte{0x02}},
		{"no upper bound", []byte{0xff, 0xff}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := prefixUpperBound(tc.prefix); !bytes.Equal(got, tc.want) {
				t.Fatalf("prefixUpperBound(%x) = %x, want %x", tc.prefix, got, tc.want)
			}
		})
	}
}
